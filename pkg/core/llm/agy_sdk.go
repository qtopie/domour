package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	antigravity "github.com/qtopie/antigravity-sdk-go"
)

type AGYSDKChatModel struct {
	agent *antigravity.Agent
	model string
}

func discoverHarnessPath(cfg *Config) string {
	// 1. Try ANTIGRAVITY_HARNESS_PATH env var
	if path := os.Getenv("ANTIGRAVITY_HARNESS_PATH"); path != "" {
		return path
	}

	// 2. Try config file via cfg.BaseURL
	if cfg != nil && cfg.BaseURL != "" && !strings.HasPrefix(cfg.BaseURL, "http://") && !strings.HasPrefix(cfg.BaseURL, "https://") {
		return cfg.BaseURL
	}

	// 3. Try to locate via sibling directories (smart dev workspace lookup)
	if cwd, err := os.Getwd(); err == nil {
		current := cwd
		for i := 0; i < 5; i++ {
			// Check if current directory has a sibling/child 'antigravity-sdk-python/localharness'
			target := filepath.Join(current, "antigravity-sdk-python", "localharness")
			if fi, err := os.Stat(target); err == nil && fi.IsDir() {
				return target
			}
			// Check if current directory has a sibling/child 'localharness'
			target = filepath.Join(current, "localharness")
			if fi, err := os.Stat(target); err == nil && fi.IsDir() {
				return target
			}

			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	// 4. Try to locate via PATH environment variable
	if pathEnv := os.Getenv("PATH"); pathEnv != "" {
		for _, dir := range filepath.SplitList(pathEnv) {
			if strings.Contains(dir, "localharness") {
				if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
					return dir
				}
			}
			target := filepath.Join(dir, "localharness")
			if fi, err := os.Stat(target); err == nil && fi.IsDir() {
				return target
			}
		}
	}

	return ""
}

func NewAGYSDKChatModel(ctx context.Context, cfg *Config) (model.ChatModel, error) {
	// Discover and set environment variable for antigravity-sdk-go
	harnessPath := discoverHarnessPath(cfg)
	if harnessPath == "" {
		return nil, fmt.Errorf("antigravity harness path not found. Please set the ANTIGRAVITY_HARNESS_PATH environment variable or specify base_url in the agy-sdk provider configuration")
	}
	os.Setenv("ANTIGRAVITY_HARNESS_PATH", harnessPath)

	if cfg != nil && cfg.APIKey != "" {
		os.Setenv("GEMINI_API_KEY", cfg.APIKey)
	}
	if cfg.ProxyURL != "" {
		os.Setenv("HTTPS_PROXY", cfg.ProxyURL)
		os.Setenv("HTTP_PROXY", cfg.ProxyURL)
	}

	agentCfg := antigravity.AgentConfig{
		SystemInstructions: "You are a helpful assistant.",
		CreateStrategy: func(tools []any, hooks []any) (antigravity.ConnectionStrategy, error) {
			return antigravity.NewLocalConnectionStrategy(antigravity.AgentConfig{
				SystemInstructions: "You are a helpful assistant.",
			}), nil
		},
	}

	agent := antigravity.NewAgent(agentCfg)
	if err := agent.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start antigravity agent: %w", err)
	}

	return &AGYSDKChatModel{
		agent: agent,
		model: cfg.Model,
	}, nil
}

func (m *AGYSDKChatModel) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	prompt := m.stringifyMessages(input)
	resp, err := m.agent.Chat(ctx, antigravity.TextContent(prompt))
	if err != nil {
		return nil, err
	}

	var fullText strings.Builder
	for chunk := range resp.Chunks() {
		if textChunk, ok := chunk.(antigravity.Text); ok {
			fullText.WriteString(textChunk.Content)
		}
	}

	return schema.AssistantMessage(fullText.String(), nil), nil
}

func (m *AGYSDKChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *AGYSDKChatModel) BindTools(_ []*schema.ToolInfo) error {
	return fmt.Errorf("agy-sdk model does not support tool binding")
}

func (m *AGYSDKChatModel) stringifyMessages(messages []*schema.Message) string {
	var parts []string
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		role := strings.ToUpper(string(msg.Role))
		if role == "" {
			role = "USER"
		}
		parts = append(parts, fmt.Sprintf("[%s]\n%s", role, msg.Content))
	}
	return strings.Join(parts, "\n\n")
}
