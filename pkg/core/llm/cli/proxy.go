package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	providerruntime "github.com/qtopie/domour/internal/provider/runtime"
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
	stopChan     chan struct{}
	resetTimer   chan struct{}
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
		stopChan:   make(chan struct{}),
		resetTimer: make(chan struct{}, 1),
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

	// Start background health monitor
	go m.runHealthMonitor()

	return m, nil
}

func (m *CLIChatModel) runHealthMonitor() {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-timer.C:
			m.performHealthCheck()
			timer.Reset(5 * time.Minute)
		case <-m.resetTimer:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(5 * time.Minute)
			
			m.mu.Lock()
			m.ready = true
			m.lastCheckErr = nil
			m.mu.Unlock()
		}
	}
}

func (m *CLIChatModel) wrapWithVProxy(ctx context.Context, command string, args []string) (*exec.Cmd, string, func()) {
	cleanup := func() {}
	if m.proxyURL == "" {
		return exec.CommandContext(ctx, command, args...), command, cleanup
	}

	// Check if vproxy is available
	vproxyPath, err := exec.LookPath("vproxy")
	if err != nil {
		// Fallback to direct execution
		return exec.CommandContext(ctx, command, args...), command, cleanup
	}

	// Create a temporary vproxy.json configuration
	tempFile, err := os.CreateTemp("", "vproxy-*.json")
	if err != nil {
		// Fallback to direct execution
		return exec.CommandContext(ctx, command, args...), command, cleanup
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

	// Wrap command with vproxy: vproxy [-v] -c <temp-config-path> <command> <args...>
	vproxyArgs := []string{}
	if m.debug {
		vproxyArgs = append(vproxyArgs, "-v")
	}
	vproxyArgs = append(vproxyArgs, "-c", configPath, command)
	vproxyArgs = append(vproxyArgs, args...)

	cmd := exec.CommandContext(ctx, vproxyPath, vproxyArgs...)
	return cmd, vproxyPath, cleanup
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
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *CLIChatModel) BindTools(_ []*schema.ToolInfo) error {
	return fmt.Errorf("%s CLI model does not support tool binding", m.command)
}

func (m *CLIChatModel) IsReady(ctx context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
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
	metadata := providerruntime.RequestMetadataFromContext(ctx)
	runtime, err := providerruntime.DefaultManager().Prepare(
		m.provider,
		metadata.SessionID,
		metadata.Workspace,
	)
	if err != nil {
		return "", err
	}

	assetsDir := filepath.Join(runtime.Workspace, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create assets dir: %w", err)
	}

	var assetPaths []string
	seenAssets := make(map[string]bool)

	for i, attachment := range attachments {
		var data []byte
		var err error

		if attachment.Base64Data != nil {
			data, err = base64Decode(*attachment.Base64Data)
			if err != nil {
				return "", fmt.Errorf("decode attachment %d: %w", i+1, err)
			}
		} else if attachment.URL != nil {
			rawURL := *attachment.URL
			if strings.HasPrefix(rawURL, "file://") {
				srcPath := strings.TrimPrefix(rawURL, "file://")
				data, err = os.ReadFile(srcPath)
				if err != nil {
					return "", fmt.Errorf("read attachment %d: %w", i+1, err)
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
				return "", fmt.Errorf("write asset %s: %w", filename, err)
			}
			seenAssets[filename] = true
		}
		assetPaths = append(assetPaths, filepath.Join("assets", filename))
	}

	args, err := m.providerImpl.GetGenerateArgs(ctx, prompt, assetPaths, runtime)
	if err != nil {
		return "", err
	}

	cmd, _, cleanup := m.wrapWithVProxy(ctx, m.command, args)
	defer cleanup()
	cmd.Env = applyProxyEnv(os.Environ(), m.proxyURL)
	if runtime.Workspace != "" {
		cmd.Dir = runtime.Workspace
	}

	if m.debug {
		fmt.Fprintf(os.Stderr, "[CLI Debug] Invoking: %s %s\n", cmd.Path, strings.Join(cmd.Args[1:], " "))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s CLI invocation failed: %w: %s", m.command, err, strings.TrimSpace(stderr.String()))
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return "", fmt.Errorf("%s CLI returned empty output", m.command)
	}
	
	select {
	case m.resetTimer <- struct{}{}:
	default:
	}

	providerruntime.DefaultManager().MarkSuccess(runtime)
	return output, nil
}

func (m *CLIChatModel) buildCLIPrompt(messages []*schema.Message) (string, []cliAttachment, error) {
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
