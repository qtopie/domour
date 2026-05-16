package llm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	providerruntime "github.com/qtopie/domour/internal/provider/runtime"
)

type CLIChatModelConfig struct {
	Provider string
	Command  string
	Model    string
	ProxyURL string
}

type CLIChatModel struct {
	provider string
	command  string
	model    string
	proxyURL string
}

func NewCLIChatModel(cfg *CLIChatModelConfig) (model.ChatModel, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cli chat model config is required")
	}

	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		return nil, fmt.Errorf("cli command is required")
	}

	command, err := resolveCLICommand(command)
	if err != nil {
		return nil, err
	}

	return &CLIChatModel{
		provider: normalizeCLIProvider(strings.TrimSpace(cfg.Provider), command),
		command:  command,
		model:    strings.TrimSpace(cfg.Model),
		proxyURL: strings.TrimSpace(cfg.ProxyURL),
	}, nil
}

func (m *CLIChatModel) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	prompt, err := m.buildCLIPrompt(input)
	if err != nil {
		return nil, err
	}
	output, err := m.invoke(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return schema.AssistantMessage(output, nil), nil
}

func (m *CLIChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *CLIChatModel) BindTools(_ []*schema.ToolInfo) error {
	return fmt.Errorf("%s CLI model does not support tool binding in the current adapter", m.command)
}

func (m *CLIChatModel) invoke(ctx context.Context, prompt string) (string, error) {
	runtime, err := providerruntime.DefaultManager().Prepare(
		m.provider,
		providerruntime.RequestMetadataFromContext(ctx).SessionID,
		providerruntime.RequestMetadataFromContext(ctx).Workspace,
	)
	if err != nil {
		return "", err
	}

	var cmd *exec.Cmd
	var env []string
	env = append(env, os.Environ()...)
	env = applyProxyEnv(env, m.proxyURL)

	switch m.provider {
	case "gemini":
		args := []string{"--prompt", prompt, "--output-format", "text", "--approval-mode", "plan"}
		if m.model != "" {
			args = append([]string{"--model", m.model}, args...)
		}
		if runtime.ConversationStarted {
			args = append(args, "--resume", "latest")
			providerruntime.DefaultManager().MarkResume(runtime)
		}
		cmd = exec.CommandContext(ctx, m.command, args...)
		env = append(env,
			"HOME="+runtime.HomeDir,
			"XDG_CONFIG_HOME="+filepathJoin(runtime.HomeDir, ".config"),
		)
	case "github-copilot-cli":
		args := []string{"--prompt", prompt, "--allow-all", "--output-format", "text", "--silent", "--config-dir", runtime.ConfigDir}
		if m.model != "" {
			args = append([]string{"--model", m.model}, args...)
		}
		if runtime.ConversationStarted {
			args = append(args, "--continue")
			providerruntime.DefaultManager().MarkResume(runtime)
		}
		cmd = exec.CommandContext(ctx, m.command, args...)
	case "qodercli":
		args := []string{"-p", prompt, "-f", "text", "-q", "--workspace", runtime.Workspace, "--dangerously-skip-permissions"}
		if m.model != "" {
			args = append(args, "--model", m.model)
		}
		if runtime.ConversationStarted {
			args = append(args, "--continue")
			providerruntime.DefaultManager().MarkResume(runtime)
		}
		cmd = exec.CommandContext(ctx, m.command, args...)
	default:
		return "", fmt.Errorf("unsupported cli provider %q", m.provider)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = env
	if runtime.Workspace != "" {
		cmd.Dir = runtime.Workspace
	}

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s CLI invocation failed: %w: %s", m.command, err, strings.TrimSpace(stderr.String()))
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return "", fmt.Errorf("%s CLI returned empty output", m.command)
	}
	providerruntime.DefaultManager().MarkSuccess(runtime)
	return output, nil
}

func (m *CLIChatModel) buildCLIPrompt(messages []*schema.Message) (string, error) {
	var parts []string
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		content, err := m.stringifyCLIMessage(msg)
		if err != nil {
			return "", err
		}
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		role := strings.ToUpper(string(msg.Role))
		if role == "" {
			role = "USER"
		}
		parts = append(parts, fmt.Sprintf("[%s]\n%s", role, content))
	}
	return strings.Join(parts, "\n\n"), nil
}

func (m *CLIChatModel) stringifyCLIMessage(msg *schema.Message) (string, error) {
	if msg == nil {
		return "", nil
	}
	if len(msg.UserInputMultiContent) == 0 {
		return msg.Content, nil
	}

	var parts []string
	for _, part := range msg.UserInputMultiContent {
		switch part.Type {
		case schema.ChatMessagePartTypeText:
			if text := strings.TrimSpace(part.Text); text != "" {
				parts = append(parts, text)
			}
		case schema.ChatMessagePartTypeImageURL:
			return "", fmt.Errorf("%s CLI adapter does not support image inputs", m.command)
		case schema.ChatMessagePartTypeAudioURL:
			return "", fmt.Errorf("%s CLI adapter does not support audio inputs", m.command)
		case schema.ChatMessagePartTypeVideoURL:
			return "", fmt.Errorf("%s CLI adapter does not support video inputs", m.command)
		default:
			return "", fmt.Errorf("%s CLI adapter does not support %q inputs", m.command, part.Type)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func resolveCLICommand(command string) (string, error) {
	checkLocal := func(cmd string) (string, bool) {
		if path, err := exec.LookPath(cmd); err == nil && path != "" {
			return cmd, true
		}
		if homeDir, err := os.UserHomeDir(); err == nil {
			localPath := filepath.Join(homeDir, ".domour", "tools", "node_modules", ".bin", cmd)
			if _, err := os.Stat(localPath); err == nil {
				return localPath, true
			}
		}
		return "", false
	}

	switch command {
	case "qodercli":
		for _, candidate := range []string{"qodercli", "qoder"} {
			if path, ok := checkLocal(candidate); ok {
				return path, nil
			}
		}
		return "", fmt.Errorf("cli command %q not found", command)
	case "github-copilot-cli":
		for _, candidate := range []string{"github-copilot-cli", "copilot"} {
			if path, ok := checkLocal(candidate); ok {
				return path, nil
			}
		}
		return "", fmt.Errorf("cli command %q not found", command)
	default:
		if path, ok := checkLocal(command); ok {
			return path, nil
		}
		return "", fmt.Errorf("cli command %q not found", command)
	}
}

func normalizeCLIProvider(provider, command string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gemini", "gemini-cli", "gemini_cli":
		return "gemini"
	case "github-copilot-cli", "copilot-cli", "github-copilot":
		return "github-copilot-cli"
	case "qodercli", "qoder-cli", "qoder":
		return "qodercli"
	default:
		switch strings.ToLower(strings.TrimSpace(command)) {
		case "gemini":
			return "gemini"
		case "copilot":
			return "github-copilot-cli"
		case "qodercli", "qoder":
			return "qodercli"
		default:
			return strings.ToLower(strings.TrimSpace(command))
		}
	}
}

func filepathJoin(parts ...string) string {
	return strings.Join(parts, string(os.PathSeparator))
}

func applyProxyEnv(env []string, proxyURL string) []string {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return env
	}

	env = append(env,
		"HTTPS_PROXY="+proxyURL,
		"https_proxy="+proxyURL,
		"HTTP_PROXY="+proxyURL,
		"http_proxy="+proxyURL,
	)
	return env
}
