package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/qtopie/domour/ark/infra/llamacpp"
	"github.com/qtopie/domour/ark/telemetry"
	acpapi "github.com/qtopie/domour/internal/app/api/acp"
	"github.com/qtopie/domour/internal/app/assistant"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/engine"
	"github.com/qtopie/domour/internal/infra/db"
	"github.com/qtopie/domour/internal/infra/llm"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "chat":
			runChatCLI(os.Args[2:])
			return
		case "acp":
			runACPServer()
			return
		case "llamacpp":
			if err := llamacpp.RunCLI(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		case "-h", "--help", "help":
			printHelp()
			return
		}
	}

	printHelp()
}

func printHelp() {
	fmt.Println("Welcome to Domour Local CLI!")
	fmt.Println("Usage: domour <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  chat      Interactive or one-shot conversational agent CLI")
	fmt.Println("  acp       Start ACP (Agent Communication Protocol) server in stdio mode")
	fmt.Println("  llamacpp  Local GGUF model runner CLI (smoke, generate, chat, forward)")
	fmt.Println()
	fmt.Println("Run 'domour <command> --help' for details on each command.")
}

func runChatCLI(args []string) {
	ctx := context.Background()

	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	providerFlag := fs.String("p", "", "LLM provider name (e.g. qwen, openai, agy-cli)")
	fs.StringVar(providerFlag, "provider", "", "LLM provider name alias")
	modelFlag := fs.String("m", "", "LLM model name")
	fs.StringVar(modelFlag, "model", "", "LLM model name alias")
	sessionFlag := fs.String("s", "cli-session", "Session ID")
	fs.StringVar(sessionFlag, "session", "cli-session", "Session ID alias")
	sysFlag := fs.String("sys", "", "System prompt override")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: domour chat [flags] [message]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	app, err := assistant.NewApp(nil, assistant.WithStore(db.NewMemoryStore()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize Domour app: %v\n", err)
		os.Exit(1)
	}

	svc, err := app.NewService()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize AssistantService: %v\n", err)
		os.Exit(1)
	}

	rest := fs.Args()
	if len(rest) > 0 {
		query := strings.Join(rest, " ")
		executeChatTurn(ctx, svc, *sessionFlag, query, *providerFlag, *modelFlag, *sysFlag)
		return
	}

	// Interactive REPL
	fmt.Println("\033[1;34m=== Domour Agent Chat CLI ===\033[0m")
	fmt.Printf("Session: %s | Provider: %s | Model: %s\n", *sessionFlag, firstNonEmpty(*providerFlag, "default"), firstNonEmpty(*modelFlag, "default"))
	fmt.Println("Type 'exit' or 'quit' to exit. Enter your message below:")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\033[1;36mYou > \033[0m")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" || line == "/exit" || line == "/quit" {
			fmt.Println("Bye!")
			break
		}

		executeChatTurn(ctx, svc, *sessionFlag, line, *providerFlag, *modelFlag, *sysFlag)
		fmt.Println()
	}
}

func executeChatTurn(ctx context.Context, svc *assistant.AssistantService, sessionID, message, provider, model, sysOverride string) {
	req := shared.MotorChatRequest{
		SessionID:            sessionID,
		Message:              message,
		SystemPromptOverride: sysOverride,
	}

	var hasThought bool
	var hasReply bool

	yield := func(ev shared.MotorStreamEvent) error {
		if ev.Content == "" {
			return nil
		}
		if ev.Type == 1 || ev.Stage == "thought" {
			if !hasThought {
				fmt.Print("\033[90m[Thinking] ")
				hasThought = true
			}
			fmt.Print(ev.Content)
			return nil
		}
		if hasThought {
			fmt.Print("\033[0m\n\n")
			hasThought = false
		}
		if ev.Type == 2 || ev.Stage == "motor" {
			fmt.Printf("\033[33m%s\033[0m", ev.Content)
			return nil
		}
		if !hasReply {
			hasReply = true
		}
		fmt.Print(ev.Content)
		return nil
	}

	err := svc.Chat(ctx, req, provider, model, yield)
	if hasThought {
		fmt.Print("\033[0m")
	}
	fmt.Println()

	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31m[Error] %v\033[0m\n", err)
	}
}

func runACPServer() {
	ctx := context.Background()

	// Initialize Telemetry (Export to stdout for debugging)
	shutdown, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "domour-acp",
		UseStdout:   false, // Set to true if you want trace output in stdout, but it might mess with JSON-RPC
	})
	if err != nil {
		log.Fatalf("Failed to setup telemetry: %v", err)
	}
	defer shutdown(ctx)

	slog.Info("Domour ACP Server starting...", "pid", os.Getpid())
	
	// Initialize LLM ChatModel for Proxy Mode
	cfg := &llm.Config{
		Provider: "agy-cli",
		Model:    "default",
		ProxyURL: "vproxy", // Use default system vproxy
		Debug:    true,
	}
	chatModel, err := llm.NewChatModel(ctx, cfg)
	if err != nil {
		slog.Error("Failed to initialize chat model", "error", err)
		os.Exit(1)
	}

	// Initialize Brain/Engine
	cognitorClient := engine.NewReloadableCognitorClient()
	executorClient, err := engine.NewConfiguredExecutorClient()
	if err != nil {
		slog.Error("Failed to initialize executor client", "error", err)
		os.Exit(1)
	}
	eng := engine.NewEngine(cognitorClient, executorClient)
	if err := eng.Start(ctx); err != nil {
		slog.Error("Failed to start engine", "error", err)
		os.Exit(1)
	}

	server := acpapi.NewServer(eng.Diencephalon(), chatModel, eng)
	transport := acpapi.NewStdioTransport()

	slog.Info("Domour ACP Server running in stdio mode", "provider", cfg.Provider, "model", cfg.Model)
	if err := server.Start(ctx, transport); err != nil {
		slog.Error("ACP Server stopped with error", "error", err)
		os.Exit(1)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
