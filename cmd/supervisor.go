package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/qtopie/domour/ark/supervisor"
	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
	"github.com/qtopie/domour/ark/supervisor/providers/chatgpt"
	"github.com/qtopie/domour/ark/supervisor/providers/generic"
)

// runSupervisorCLI 执行 domour supervisor 命令行入口
func runSupervisorCLI(args []string) error {
	if len(args) == 0 {
		printSupervisorHelp()
		return nil
	}

	subcmd := args[0]
	subargs := args[1:]

	switch subcmd {
	case "run":
		return runAgentInteractive(subargs)
	case "daemon", "server":
		return runDaemon(subargs)
	case "list", "ps":
		return runList(subargs)
	case "inspect", "show":
		return runInspect(subargs)
	case "approve":
		return runApprove(subargs, true)
	case "deny":
		return runApprove(subargs, false)
	case "resume":
		return runResume(subargs)
	case "-h", "--help", "help":
		printSupervisorHelp()
		return nil
	default:
		fmt.Fprintf(os.Stderr, "Unknown supervisor command: %s\n\n", subcmd)
		printSupervisorHelp()
		return fmt.Errorf("unknown subcommand %s", subcmd)
	}
}

func printSupervisorHelp() {
	fmt.Println("Usage: domour supervisor <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  run       Launch and interactively control an Agent process (default: antigravity)")
	fmt.Println("  daemon    Start local supervisor telemetry & approval daemon")
	fmt.Println("  list      List currently running Agent processes discovered on this machine")
	fmt.Println("  inspect   Inspect recent turns and outputs of a running or completed session")
	fmt.Println("  approve   Approve a pending tool confirmation for an agent session")
	fmt.Println("  deny      Deny a pending tool confirmation for an agent session")
	fmt.Println("  resume    Resume an existing conversation session with a prompt")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  domour supervisor list")
	fmt.Println("  domour supervisor inspect <session_id>                     # auto-detect provider")
	fmt.Println("  domour supervisor inspect -p antigravity -n 10 <session_id> # explicit provider & turns")
	fmt.Println("  domour supervisor run \"Inspect git status\"")
	fmt.Println("  domour supervisor daemon")
}


func runAgentInteractive(args []string) error {
	fs := flag.NewFlagSet("supervisor run", flag.ExitOnError)
	providerFlag := fs.String("provider", "antigravity", "Agent provider (antigravity, generic)")
	fs.StringVar(providerFlag, "p", "antigravity", "Alias for --provider")
	wsFlag := fs.String("workspace", "", "Workspace path (default: current directory)")
	fs.StringVar(wsFlag, "w", "", "Alias for --workspace")
	sessionFlag := fs.String("session", "", "Optional session/conversation ID")
	fs.StringVar(sessionFlag, "s", "", "Alias for --session")
	skipPermFlag := fs.Bool("dangerously-skip-permissions", true, "Auto approve tool permissions")
	commandFlag := fs.String("cmd", "", "Override binary command name (e.g. agy)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: domour supervisor run [flags] [prompt]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	workspace := *wsFlag
	if workspace == "" {
		cwd, err := os.Getwd()
		if err == nil {
			workspace = cwd
		}
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err == nil {
		workspace = absWorkspace
	}

	var provider supervisor.AgentProvider
	switch *providerFlag {
	case "antigravity", "agy":
		provider = antigravity.NewProvider()
	case "chatgpt", "codex", "openai":
		provider = chatgpt.NewProvider()
	default:
		provider = generic.NewProvider()
	}

	command := *commandFlag
	if command == "" {
		if provider.Name() == antigravity.ProviderName {
			command = "agy"
		} else if provider.Name() == chatgpt.ProviderName {
			command = "codex"
		}
	}

	reg := supervisor.NewRegistry()
	ctrl := supervisor.NewController(reg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n\033[1;33m[Supervisor] Terminating agent process...\033[0m")
		cancel()
	}()

	fmt.Printf("\033[1;36m[Supervisor] Launching managed %s agent in %s...\033[0m\n", provider.Name(), workspace)

	mp, err := ctrl.LaunchHeadless(ctx, provider, command, supervisor.LaunchOptions{
		Workspace:                  workspace,
		SessionID:                  *sessionFlag,
		DangerouslySkipPermissions: *skipPermFlag,
	})
	if err != nil {
		return fmt.Errorf("failed to launch agent process: %w", err)
	}
	defer ctrl.StopProcess(mp.ID)

	turnDoneCh := make(chan struct{}, 10)
	go func() {
		scanner := bufio.NewScanner(mp.Stdout)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			// 1. Antigravity 原生 stream-json 兼容
			var base map[string]interface{}
			if err := json.Unmarshal(line, &base); err == nil {
				if event, ok := base["event"].(string); ok {
					switch event {
					case "init":
						convID, _ := base["conversation_id"].(string)
						fmt.Printf("\033[1;32m[Agent Connected] Session ID: %s\033[0m\n", convID)
						continue
					case "step_update":
						su, ok := base["step_update"].(map[string]interface{})
						if ok {
							stepType, _ := su["step_type"].(string)
							state, _ := su["state"].(string)
							if stepType == "tool" && state == "ACTIVE" {
								toolName, _ := su["tool_name"].(string)
								fmt.Printf("\n\033[1;33m[Tool Calling] ⚙️  %s\033[0m", toolName)
								if toolInfo, ok := su["tool_info"].(map[string]interface{}); ok {
									if params, ok := toolInfo["parameters"].(map[string]interface{}); ok {
										if cmd, ok := params["CommandLine"].(string); ok && cmd != "" {
											fmt.Printf(" -> \033[33m%s\033[0m", cmd)
										}
									}
								}
								fmt.Println()
							} else if stepType == "agent_response" {
								if delta, ok := su["text_delta"].(string); ok && delta != "" {
									fmt.Print(delta)
								}
							}
						}
						continue
					case "result":
						fmt.Println("\n\033[1;32m[Turn Completed]\033[0m")
						select {
						case turnDoneCh <- struct{}{}:
						default:
						}
						continue
					}
				}
			}

			// 2. 统一 Provider 结构化事件解析 (支持 ChatGPT/Codex 及其他 Provider)
			rec, err := provider.ParseTranscriptLine(line)
			if err == nil && rec != nil {
				switch rec.Type {
				case "thread_started":
					fmt.Printf("\033[1;32m[Agent Connected] Thread ID: %s\033[0m\n", rec.Content)
				case "turn_started":
					// 新轮次启动
				case "assistant_response", "assistant_delta":
					if rec.Content != "" {
						fmt.Print(rec.Content)
					}
				case "tool_use":
					for _, tc := range rec.ToolCalls {
						fmt.Printf("\n\033[1;33m[Tool Calling] ⚙️  %s\033[0m", tc.ToolName)
						if cmd, ok := tc.Arguments["command"].(string); ok && cmd != "" {
							fmt.Printf(" -> \033[33m%s\033[0m", cmd)
						}
						fmt.Println()
					}
				case "tool_result":
					// 工具结果
				case "turn_completed":
					fmt.Println("\n\033[1;32m[Turn Completed]\033[0m")
					select {
					case turnDoneCh <- struct{}{}:
					default:
					}
				case "error":
					fmt.Printf("\n\033[1;31m[Agent Error] %s\033[0m\n", rec.Content)
					select {
					case turnDoneCh <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	go func() {
		scanner := bufio.NewScanner(mp.Stderr)
		for scanner.Scan() {
			txt := strings.TrimSpace(scanner.Text())
			if txt != "" {
				fmt.Fprintf(os.Stderr, "\033[1;31m[Agent Stderr] %s\033[0m\n", txt)
			}
		}
	}()

	rest := fs.Args()
	if len(rest) > 0 {
		initialPrompt := strings.Join(rest, " ")
		fmt.Printf("\033[1;34m[Prompt] %s\033[0m\n", initialPrompt)
		if err := ctrl.SendInstruction(ctx, mp.ID, initialPrompt); err != nil {
			return fmt.Errorf("failed to send initial instruction: %w", err)
		}
		select {
		case <-turnDoneCh:
		case <-ctx.Done():
			return nil
		}
	}

	fmt.Println("\n\033[1;35m=== Domour Agent Control Console (Type 'exit' to quit) ===\033[0m")
	stdinScanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("\n\033[1;34m[supervisor]> \033[0m")
		if !stdinScanner.Scan() {
			break
		}

		line := strings.TrimSpace(stdinScanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			fmt.Println("Exiting supervisor...")
			break
		}

		if err := ctrl.SendInstruction(ctx, mp.ID, line); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to send instruction: %v\n", err)
			continue
		}

		select {
		case <-turnDoneCh:
		case <-ctx.Done():
			return nil
		}
	}

	return nil
}

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("supervisor daemon", flag.ExitOnError)
	sockFlag := fs.String("socket", "/tmp/domour-supervisor.sock", "Unix domain socket path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	supv, err := supervisor.New(supervisor.Config{SocketPath: *sockFlag})
	if err != nil {
		return err
	}
	_ = supv.RegisterProvider(antigravity.NewProvider())
	_ = supv.RegisterProvider(chatgpt.NewProvider())
	_ = supv.RegisterProvider(generic.NewProvider())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	if err := supv.Start(ctx); err != nil {
		return err
	}
	fmt.Printf("\033[1;32m[Supervisor Daemon] Listening on %s...\033[0m\n", *sockFlag)

	<-sigCh
	fmt.Println("\nShutting down supervisor daemon...")
	return supv.Stop()
}

func runList(args []string) error {
	fmt.Printf("\033[1;34mScanning active agent processes on this machine...\033[0m\n\n")

	sessions, err := antigravity.DiscoverRunningSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Discovery failed: %v\n", err)
	}

	chatgptSessions, _ := chatgpt.DiscoverRunningSessions("")

	totalSessions := len(sessions) + len(chatgptSessions)
	if totalSessions == 0 {
		fmt.Println("No active Agent processes detected holding locks or running in background.")
		fmt.Println("\nTip: Run 'domour supervisor run' to launch a managed agent, or start 'agy' / 'codex' in any workspace.")
		return nil
	}

	if len(sessions) > 0 {
		fmt.Printf("\033[1;32mFound %d active Antigravity session(s):\033[0m\n\n", len(sessions))
		fmt.Printf("%-8s %-12s %-8s %-38s %-32s\n", "PID", "STATUS", "STEPS", "SESSION ID", "WORKSPACE")
		fmt.Println(strings.Repeat("-", 100))

		for _, s := range sessions {
			statusColor := "\033[32m" // green
			if s.Process.Status == supervisor.StatusWorking || s.Process.Status == supervisor.StatusToolUse {
				statusColor = "\033[33m" // yellow
			} else if s.Process.Status == supervisor.StatusAwaitingConfirmation {
				statusColor = "\033[1;31m" // bold red
			}

			workspaceShort := s.Process.Workspace
			homeDir, _ := os.UserHomeDir()
			if homeDir != "" && strings.HasPrefix(workspaceShort, homeDir) {
				workspaceShort = "~" + strings.TrimPrefix(workspaceShort, homeDir)
			}

			fmt.Printf("%-8d %s%-12s\033[0m %-8d %-38s %-32s\n",
				s.Process.PID,
				statusColor,
				s.Process.Status,
				s.LastStep,
				s.Process.ID,
				workspaceShort,
			)

			if s.LastContent != "" {
				snippet := strings.ReplaceAll(s.LastContent, "\n", " ")
				if len([]rune(snippet)) > 90 {
					snippet = string([]rune(snippet)[:90]) + "..."
				}
				fmt.Printf("   ↳ \033[90mLatest:\033[0m %s\n", snippet)
			}
			if s.Process.CurrentTool != "" {
				fmt.Printf("   ↳ \033[33mCurrent Tool:\033[0m %s\n", s.Process.CurrentTool)
			}
		}
		fmt.Println()
	}

	if len(chatgptSessions) > 0 {
		fmt.Printf("\033[1;32mFound %d active ChatGPT / Codex session(s):\033[0m\n\n", len(chatgptSessions))
		fmt.Printf("%-8s %-12s %-38s %-32s\n", "PID", "STATUS", "SESSION ID", "WORKSPACE")
		fmt.Println(strings.Repeat("-", 90))

		for _, p := range chatgptSessions {
			statusColor := "\033[32m"
			if p.Status == supervisor.StatusWorking {
				statusColor = "\033[33m"
			}
			workspaceShort := p.Workspace
			homeDir, _ := os.UserHomeDir()
			if homeDir != "" && strings.HasPrefix(workspaceShort, homeDir) {
				workspaceShort = "~" + strings.TrimPrefix(workspaceShort, homeDir)
			}
			fmt.Printf("%-8d %s%-12s\033[0m %-38s %-32s\n",
				p.PID,
				statusColor,
				p.Status,
				p.ID,
				workspaceShort,
			)
		}
		fmt.Println()
	}

	fmt.Println("\nTip: Run 'domour supervisor inspect <session_id>' to inspect recent conversation turns.")
	return nil
}

func runInspect(args []string) error {
	fs := flag.NewFlagSet("supervisor inspect", flag.ContinueOnError)
	providerFlag := fs.String("provider", "", "Explicitly specify agent provider (e.g. antigravity, generic)")
	fs.StringVar(providerFlag, "p", "", "Alias for --provider")
	turnsFlag := fs.Int("turns", 5, "Number of recent turns to display")
	fs.IntVar(turnsFlag, "n", 5, "Alias for --turns")

	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("usage: domour supervisor inspect [-p <provider>] [-n <turns>] <session_id>")
	}

	targetID := rest[0]
	maxTurns := *turnsFlag
	if maxTurns <= 0 {
		maxTurns = 5
	}

	providers := []supervisor.AgentProvider{
		antigravity.NewProvider(),
		chatgpt.NewProvider(),
		generic.NewProvider(),
	}

	res, err := supervisor.InspectSession(providers, *providerFlag, targetID, maxTurns)
	if err != nil {
		return fmt.Errorf("inspect failed: %w", err)
	}

	fmt.Printf("\033[1;36m=== Inspecting Session: %s [%s] (Latest %d turns) ===\033[0m\n", res.SessionID, res.ProviderName, len(res.Records))
	if res.TranscriptPath != "" {
		fmt.Printf("\033[90mTranscript: %s\033[0m\n\n", res.TranscriptPath)
	}

	for _, rec := range res.Records {
		roleHeader := fmt.Sprintf("[%s | Step %d]", strings.ToUpper(rec.Role), rec.StepIndex)
		if rec.Role == "user" {
			fmt.Printf("\033[1;34m%s\033[0m\n", roleHeader)
		} else {
			fmt.Printf("\033[1;32m%s\033[0m\n", roleHeader)
		}

		if rec.Thinking != "" {
			fmt.Printf("\033[35m[Thinking]\033[0m %s\n", rec.Thinking)
		}

		for _, tc := range rec.ToolCalls {
			fmt.Printf("\033[33m[Tool Call]\033[0m %s", tc.ToolName)
			if cmd, ok := tc.Arguments["CommandLine"].(string); ok && cmd != "" {
				fmt.Printf(" -> %s", cmd)
			}
			fmt.Println()
		}

		if rec.Content != "" {
			fmt.Printf("%s\n", rec.Content)
		}
		fmt.Println(strings.Repeat("-", 60))
	}

	return nil
}


func runApprove(args []string, allow bool) error {
	if len(args) == 0 {
		return fmt.Errorf("session ID is required")
	}
	sessionID := args[0]
	action := "approve"
	if !allow {
		action = "deny"
	}
	fmt.Printf("[%s] Submitted %s for session %s (if daemon is active)\n", strings.ToUpper(action), action, sessionID)
	return nil
}

func runResume(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: domour supervisor resume <session_id> <prompt>")
	}
	sessionID := args[0]
	prompt := strings.Join(args[1:], " ")

	fmt.Printf("\033[1;36m[Supervisor] Resuming session %s with prompt: %s\033[0m\n", sessionID, prompt)

	provider := antigravity.NewProvider()
	resumeArgs, err := provider.BuildResumeArgs(sessionID, prompt, supervisor.LaunchOptions{
		DangerouslySkipPermissions: true,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Command: agy %s\n", strings.Join(resumeArgs, " "))
	return nil
}
