package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	Provider string
	Command  string
	Model    string
	ProxyURL string
	BaseURL  string
	APIKey   string
	Debug    bool
}

type cliProvider interface {
	GetGenerateArgs(ctx context.Context, prompt string, assetPaths []string, runtime *providerruntime.SessionRuntime) ([]string, error)
	HealthCheck(ctx context.Context) (string, error)
}

type CLIChatModel struct {
	provider     string
	command      string
	model        string
	proxyURL     string
	debug        bool
	providerImpl cliProvider

	mu           sync.RWMutex
	ready        bool
	lastCheckErr error
	stats        string
}

func New(cfg *Config) (model.ChatModel, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cli chat model config is required")
	}

	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		return nil, fmt.Errorf("cli command is required")
	}

	resolvedCmd, err := resolveCLICommand(command)
	if err != nil {
		return nil, err
	}

	provider := normalizeCLIProvider(strings.TrimSpace(cfg.Provider), resolvedCmd)
	m := &CLIChatModel{
		provider:   provider,
		command:    resolvedCmd,
		model:      strings.TrimSpace(cfg.Model),
		proxyURL:   strings.TrimSpace(cfg.ProxyURL),
		debug:      cfg.Debug,
	}

	switch provider {
	case "gemini":
		m.providerImpl = &geminiProvider{command: resolvedCmd, model: m.model, proxyURL: m.proxyURL}
	case "agy-cli", "agy":
		m.providerImpl = &agyProvider{command: resolvedCmd, model: m.model, proxyURL: m.proxyURL}
	case "agy-sdk":
		harnessPath := discoverHarnessPath(cfg.BaseURL)
		m.providerImpl = &agyProvider{
			command:     resolvedCmd,
			model:       m.model,
			proxyURL:    m.proxyURL,
			apiKey:      cfg.APIKey,
			harnessPath: harnessPath,
			isSDKMode:   true,
		}
	case "github-copilot-cli":
		m.providerImpl = &copilotProvider{command: resolvedCmd, model: m.model, proxyURL: m.proxyURL}
	case "qodercli":
		m.providerImpl = &qoderProvider{command: resolvedCmd, model: m.model, proxyURL: m.proxyURL}
	case "claude":
		m.providerImpl = &claudeProvider{command: resolvedCmd, model: m.model, proxyURL: m.proxyURL}
	default:
		return nil, fmt.Errorf("unsupported cli provider %q", provider)
	}

	return m, nil
}

func (m *CLIChatModel) wrapWithVProxy(ctx context.Context, command string, args []string) (*exec.Cmd, string, func()) {
	cleanup := func() {}

	// If no proxy is requested, execute directly
	if m.proxyURL == "" {
		return exec.CommandContext(ctx, command, args...), command, cleanup
	}

	// Check if vproxy is available in PATH
	vproxyPath, err := exec.LookPath("vproxy")
	if err != nil {
		// vproxy not found, fallback to direct execution
		return exec.CommandContext(ctx, command, args...), command, cleanup
	}

	// Case 1: Use vproxy with its default/global config (ProxyURL is exactly "vproxy")
	if m.proxyURL == "vproxy" || m.proxyURL == "default" {
		vproxyArgs := []string{}
		if m.debug {
			vproxyArgs = append(vproxyArgs, "-v")
		}
		vproxyArgs = append(vproxyArgs, command)
		vproxyArgs = append(vproxyArgs, args...)

		return exec.CommandContext(ctx, vproxyPath, vproxyArgs...), vproxyPath, cleanup
	}

	// Case 2: ProxyURL is a specific URL - Generate temporary config
	tempFile, err := os.CreateTemp("", "vproxy-*.json")
	if err != nil {
		// Fallback to vproxy with default config if temp file fails
		return exec.CommandContext(ctx, vproxyPath, append([]string{command}, args...)...), vproxyPath, cleanup
	}

	configData := map[string]any{
		"upstreams": []string{m.proxyURL},
		"rules": []string{
			fmt.Sprintf("PROCESS,%s,PROXY", filepath.Base(command)),
			"FINAL,PROXY",
		},
		"test_interval": 30,
	}

	jsonData, err := json.MarshalIndent(configData, "", "  ")
	if err == nil {
		_, _ = tempFile.Write(jsonData)
	}
	_ = tempFile.Close()

	configPath := tempFile.Name()
	cleanup = func() {
		_ = os.Remove(configPath)
	}

	vproxyArgs := []string{}
	if m.debug {
		vproxyArgs = append(vproxyArgs, "-v")
	}
	vproxyArgs = append(vproxyArgs, "-c", configPath, command)
	vproxyArgs = append(vproxyArgs, args...)

	return exec.CommandContext(ctx, vproxyPath, vproxyArgs...), vproxyPath, cleanup
}

func (m *CLIChatModel) performHealthCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stats, err := m.providerImpl.HealthCheck(ctx)
	if err == nil && stats != "" {
		m.mu.Lock()
		m.ready = true
		m.lastCheckErr = nil
		m.stats = stats
		m.mu.Unlock()
		return
	}

	var args []string
	switch m.provider {
	case "qodercli":
		args = []string{"status"}
	default:
		args = []string{"--version"}
	}

	cmd, _, cleanup := m.wrapWithVProxy(ctx, m.command, args)
	defer cleanup()
	cmd.Env = applyProxyEnv(os.Environ(), m.proxyURL)

	if m.debug {
		fmt.Fprintf(os.Stderr, "[CLI Debug] Health check: %s %s\n", cmd.Path, strings.Join(cmd.Args[1:], " "))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	
	output := strings.TrimSpace(stdout.String())
	if runErr != nil {
		runErr = fmt.Errorf("%w: %s", runErr, strings.TrimSpace(stderr.String()))
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if runErr != nil {
		fmt.Printf("[CLI Health Check] %s failed: %v\n", m.command, runErr)
		m.ready = false
		m.lastCheckErr = fmt.Errorf("%s health check failed: %w", m.command, runErr)
		return
	}

	m.ready = true
	m.lastCheckErr = nil
	if stats != "" {
		m.stats = stats
	} else {
		m.stats = output
	}
	fmt.Printf("[CLI Health Check] %s succeeded: stats=%s\n", m.command, m.stats)
}

func (m *CLIChatModel) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	prompt, attachments, err := m.buildCLIPrompt(input)
	if err != nil {
		return nil, err
	}
	output, err := m.invoke(ctx, prompt, attachments)
	if err != nil {
		return nil, err
	}
	return schema.AssistantMessage(output, nil), nil
}

func (m *CLIChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	prompt, attachments, err := m.buildCLIPrompt(input)
	if err != nil {
		return nil, err
	}

	sr, sw := schema.Pipe[*schema.Message](100)

	go func() {
		defer sw.Close()

		metadata := providerruntime.RequestMetadataFromContext(ctx)
		runtime, err := providerruntime.DefaultManager().Prepare(
			m.provider,
			metadata.SessionID,
			metadata.Workspace,
		)
		if err != nil {
			sw.Send(nil, err)
			return
		}

		assetsDir := filepath.Join(runtime.Workspace, "assets")
		if err := os.MkdirAll(assetsDir, 0o755); err != nil {
			sw.Send(nil, fmt.Errorf("failed to create assets dir: %w", err))
			return
		}

		assetPaths, err := m.prepareAssets(assetsDir, attachments)
		if err != nil {
			sw.Send(nil, err)
			return
		}

		args, err := m.providerImpl.GetGenerateArgs(ctx, prompt, assetPaths, runtime)
		if err != nil {
			sw.Send(nil, err)
			return
		}

		cmd, _, cleanup := m.wrapWithVProxy(ctx, m.command, args)
		defer cleanup()

		if m.proxyURL != "" && strings.Contains(cmd.Path, "vproxy") {
			cmd.Env = os.Environ()
		} else {
			cmd.Env = applyProxyEnv(os.Environ(), m.proxyURL)
		}

		if runtime.Workspace != "" {
			if _, err := os.Stat(runtime.Workspace); err == nil {
				cmd.Dir = runtime.Workspace
			}
		}

		if m.debug {
			quotedArgs := make([]string, len(cmd.Args))
			for i, arg := range cmd.Args {
				quotedArgs[i] = fmt.Sprintf("%q", arg)
			}
			fmt.Fprintf(os.Stderr, "[CLI Debug] Stream Invoking: %s\n", strings.Join(quotedArgs, " "))
		} else {
			fmt.Fprintf(os.Stderr, "[CLI] Stream Executing %s...\n", m.command)
		}

		cmd.Stderr = os.Stderr

		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			sw.Send(nil, err)
			return
		}

		if err := cmd.Start(); err != nil {
			sw.Send(nil, err)
			return
		}

		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			text := scanner.Text()
			text = StripANSI(text)
			if strings.HasPrefix(text, "Warning:") || strings.HasPrefix(text, "Warning ") {
				continue
			}
			sw.Send(schema.AssistantMessage(text+"\n", nil), nil)
		}

		if err := cmd.Wait(); err != nil {
			sw.Send(nil, err)
			return
		}

		m.mu.Lock()
		m.ready = true
		m.lastCheckErr = nil
		m.mu.Unlock()

		providerruntime.DefaultManager().MarkSuccess(runtime)
	}()

	return sr, nil
}

func (m *CLIChatModel) BindTools(_ []*schema.ToolInfo) error {
	return fmt.Errorf("%s CLI model does not support tool binding", m.command)
}

func (m *CLIChatModel) IsReady(ctx context.Context) (bool, error) {
	m.mu.Lock()
	if !m.ready && m.lastCheckErr == nil {
		m.mu.Unlock()
		m.performHealthCheck()
		m.mu.Lock()
	}
	defer m.mu.Unlock()
	return m.ready, m.lastCheckErr
}

func (m *CLIChatModel) GetAPIHealth() *GeminiAPIHealth {
	if gp, ok := m.providerImpl.(*geminiProvider); ok {
		health, _ := gp.GetQuotas(context.Background())
		return health
	}
	return nil
}

type cliAttachment struct {
	Base64Data *string
	URL        *string
	MIMEType   string
}

func (m *CLIChatModel) invoke(ctx context.Context, prompt string, attachments []cliAttachment) (string, error) {
	tracer := otel.Tracer("domour.llm.cli")
	ctx, span := tracer.Start(ctx, "CLIChatModel.invoke", trace.WithAttributes(
		attribute.String("provider", m.provider),
		attribute.String("command", m.command),
	))
	defer span.End()

	metadata := providerruntime.RequestMetadataFromContext(ctx)
	runtime, err := providerruntime.DefaultManager().Prepare(
		m.provider,
		metadata.SessionID,
		metadata.Workspace,
	)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	assetsDir := filepath.Join(runtime.Workspace, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to create assets dir: %w", err)
	}

	assetPaths, err := m.prepareAssets(assetsDir, attachments)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	args, err := m.providerImpl.GetGenerateArgs(ctx, prompt, assetPaths, runtime)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	cmd, _, cleanup := m.wrapWithVProxy(ctx, m.command, args)
	defer cleanup()

	// If using vproxy, we should NOT set proxy env vars as they might confuse the underlying tool
	// or conflict with vproxy's transparent redirection.
	if m.proxyURL != "" && strings.Contains(cmd.Path, "vproxy") {
		// Just use default env, don't inject proxies
		cmd.Env = os.Environ()
	} else {
		cmd.Env = applyProxyEnv(os.Environ(), m.proxyURL)
	}

	if runtime.Workspace != "" {
		// Ensure workspace exists before setting it as Dir
		if _, err := os.Stat(runtime.Workspace); err == nil {
			cmd.Dir = runtime.Workspace
		}
	}

	if m.debug {
		// Log arguments with %q to see exactly how they are quoted
		quotedArgs := make([]string, len(cmd.Args))
		for i, arg := range cmd.Args {
			quotedArgs[i] = fmt.Sprintf("%q", arg)
		}
		fmt.Fprintf(os.Stderr, "[CLI Debug] Invoking: %s\n", strings.Join(quotedArgs, " "))
	} else {
		fmt.Fprintf(os.Stderr, "[CLI] Executing %s...\n", m.command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		err = fmt.Errorf("%s CLI invocation failed: %w: %s", m.command, err, errMsg)
		slog.Error("CLI invocation failed", "error", err, "stderr", errMsg)
		span.RecordError(err)
		return "", err
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		errMsg := strings.TrimSpace(stderr.String())
		slog.Warn("CLI returned empty output", "stderr", errMsg)
		return "", fmt.Errorf("%s CLI returned empty output. Stderr: %s", m.command, errMsg)
	}

	// Remove ANSI escape codes (colors, etc.)
	output = StripANSI(output)

	// Filter out common CLI warnings that might be captured in stdout
	lines := strings.Split(output, "\n")
	var filteredLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "Warning:") || strings.HasPrefix(line, "Warning ") {
			slog.Warn("CLI Warning captured in stdout", "warning", line)
			fmt.Fprintf(os.Stderr, "[CLI Warning] %s\n", line)
			continue
		}
		filteredLines = append(filteredLines, line)
	}
	output = strings.TrimSpace(strings.Join(filteredLines, "\n"))

	if output == "" {
		errMsg := strings.TrimSpace(stdout.String())
		slog.Error("CLI only returned warnings", "output", errMsg)
		return "", fmt.Errorf("%s CLI only returned warnings: %s", m.command, errMsg)
	}
	
	m.mu.Lock()
	m.ready = true
	m.lastCheckErr = nil
	m.mu.Unlock()

	slog.Info("CLI invocation succeeded", "provider", m.provider, "output_len", len(output))
	providerruntime.DefaultManager().MarkSuccess(runtime)
	return output, nil
}

func (m *CLIChatModel) buildCLIPrompt(messages []*schema.Message) (string, []cliAttachment, error) {
	if len(messages) == 1 {
		// Strictly return only content for single-message requests to ensure CLI compatibility
		content, attachments, err := m.stringifyCLIMessage(messages[0])
		if err != nil {
			return "", nil, err
		}
		return strings.TrimSpace(content), attachments, nil
	}

	var parts []string
	var allAttachments []cliAttachment
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		content, attachments, err := m.stringifyCLIMessage(msg)
		if err != nil {
			return "", nil, err
		}
		allAttachments = append(allAttachments, attachments...)
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
	return strings.Join(parts, "\n\n"), allAttachments, nil
}

// StripANSI removes ANSI escape codes from a string
func StripANSI(str string) string {
	const ansi = "[\u001B\u009B][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]"
	re := regexp.MustCompile(ansi)
	return re.ReplaceAllString(str, "")
}

func (m *CLIChatModel) stringifyCLIMessage(msg *schema.Message) (string, []cliAttachment, error) {
	if msg == nil {
		return "", nil, nil
	}
	if len(msg.UserInputMultiContent) == 0 {
		return msg.Content, nil, nil
	}

	var parts []string
	var attachments []cliAttachment
	for _, part := range msg.UserInputMultiContent {
		switch part.Type {
		case schema.ChatMessagePartTypeText:
			if text := strings.TrimSpace(part.Text); text != "" {
				parts = append(parts, text)
			}
		case schema.ChatMessagePartTypeImageURL:
			if part.Image != nil {
				attachments = append(attachments, cliAttachment{
					Base64Data: part.Image.Base64Data,
					URL:        part.Image.URL,
					MIMEType:   part.Image.MIMEType,
				})
			}
		default:
			parts = append(parts, fmt.Sprintf("[%s CLI adapter does not support %q inputs, skipping]", m.command, part.Type))
		}
	}
	return strings.Join(parts, "\n\n"), attachments, nil
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
	case "agy":
		for _, candidate := range []string{"agy"} {
			if path, ok := checkLocal(candidate); ok {
				return path, nil
			}
		}
		return "", fmt.Errorf("cli command %q not found", command)
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
	case "claude":
		for _, candidate := range []string{"claude", "claude-code"} {
			if path, ok := checkLocal(candidate); ok {
				return path, nil
			}
		}
		if homeDir, err := os.UserHomeDir(); err == nil {
			downloadsDir := filepath.Join(homeDir, ".claude", "downloads")
			if files, err := filepath.Glob(filepath.Join(downloadsDir, "claude-*")); err == nil && len(files) > 0 {
				for i := len(files) - 1; i >= 0; i-- {
					if info, err := os.Stat(files[i]); err == nil && !info.IsDir() {
						return files[i], nil
					}
				}
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
	case "agy", "agy-cli", "agy_cli":
		return "agy-cli"
	case "github-copilot-cli", "copilot-cli", "github-copilot":
		return "github-copilot-cli"
	case "qodercli", "qoder-cli", "qoder":
		return "qodercli"
	case "claude", "claude-code", "claude_cli":
		return "claude"
	default:
		switch strings.ToLower(strings.TrimSpace(command)) {
		case "gemini":
			return "gemini"
		case "agy":
			return "agy-cli"
		case "copilot":
			return "github-copilot-cli"
		case "qodercli", "qoder":
			return "qodercli"
		case "claude":
			return "claude"
		default:
			return strings.ToLower(strings.TrimSpace(command))
		}
	}
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

func base64Decode(data string) ([]byte, error) {
	data = strings.TrimSpace(data)
	if idx := strings.Index(data, ","); idx != -1 {
		data = data[idx+1:]
	}
	return base64.StdEncoding.DecodeString(data)
}

func (m *CLIChatModel) prepareAssets(assetsDir string, attachments []cliAttachment) ([]string, error) {
	var assetPaths []string
	seenAssets := make(map[string]bool)

	for i, attachment := range attachments {
		var data []byte
		var err error

		if attachment.Base64Data != nil {
			data, err = base64Decode(*attachment.Base64Data)
			if err != nil {
				return nil, fmt.Errorf("decode attachment %d: %w", i+1, err)
			}
		} else if attachment.URL != nil {
			rawURL := *attachment.URL
			if strings.HasPrefix(rawURL, "file://") {
				srcPath := strings.TrimPrefix(rawURL, "file://")
				data, err = os.ReadFile(srcPath)
				if err != nil {
					return nil, fmt.Errorf("read attachment %d: %w", i+1, err)
				}
			} else {
				continue
			}
		}

		if len(data) == 0 {
			continue
		}

		hash := sha256.Sum256(data)
		assetUUID := uuid.NewSHA1(uuid.NameSpaceDNS, hash[:]).String()
		ext := ".bin"
		if parts := strings.Split(attachment.MIMEType, "/"); len(parts) == 2 {
			ext = "." + parts[1]
		}
		filename := assetUUID + ext
		targetPath := filepath.Join(assetsDir, filename)

		if !seenAssets[filename] {
			if err := os.WriteFile(targetPath, data, 0o644); err != nil {
				return nil, fmt.Errorf("write asset %s: %w", filename, err)
			}
			seenAssets[filename] = true
		}
		assetPaths = append(assetPaths, filepath.Join("assets", filename))
	}
	return assetPaths, nil
}
