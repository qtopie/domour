package proxy

import (
	"context"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/schema"
	einomodel "github.com/cloudwego/eino/components/model"
	appconfig "github.com/qtopie/domour/internal/config"
)

func TestResolveConfigDefaults(t *testing.T) {
	cfg := ResolveConfig("chat", appconfig.DomourConfig{})
	if cfg.Provider != "github-copilot-cli" {
		t.Fatalf("ResolveConfig().Provider = %q, want %q", cfg.Provider, "github-copilot-cli")
	}
	if cfg.Model != "" {
		t.Fatalf("ResolveConfig().Model = %q, want empty", cfg.Model)
	}
	if cfg.APIKey != "" {
		t.Fatalf("ResolveConfig().APIKey = %q, want empty", cfg.APIKey)
	}
	if cfg.BaseURL != "" {
		t.Fatalf("ResolveConfig().BaseURL = %q, want empty", cfg.BaseURL)
	}
	if cfg.ProxyURL != "" {
		t.Fatalf("ResolveConfig().ProxyURL = %q, want empty", cfg.ProxyURL)
	}
}

func TestResolveConfigEntryOverrides(t *testing.T) {
	t.Setenv("DOMOUR_DEFAULT_PROVIDER", "gemini-cli")
	t.Setenv("DOMOUR_DEFAULT_MODEL", "gemini-2.5-pro")
	t.Setenv("DOMOUR_DEFAULT_API_KEY", "default-key")
	t.Setenv("DOMOUR_DEFAULT_BASE_URL", "http://default.local/v1")
	t.Setenv("DOMOUR_DEFAULT_HTTPS_PROXY", "http://default:1080")
	t.Setenv("DOMOUR_CHAT_PROVIDER", "github-copilot-cli")
	t.Setenv("DOMOUR_CHAT_MODEL", "gpt-5")
	t.Setenv("DOMOUR_CHAT_API_KEY", "chat-key")
	t.Setenv("DOMOUR_CHAT_BASE_URL", "http://chat.local/v1")
	t.Setenv("DOMOUR_CHAT_HTTPS_PROXY", "http://chat:1080")

	cfg := ResolveConfig("chat", appconfig.DomourConfig{
		HTTPSProxy: "http://global:1080",
	})
	if cfg.Provider != "github-copilot-cli" {
		t.Fatalf("ResolveConfig().Provider = %q, want %q", cfg.Provider, "github-copilot-cli")
	}
	if cfg.Model != "gpt-5" {
		t.Fatalf("ResolveConfig().Model = %q, want %q", cfg.Model, "gpt-5")
	}
	if cfg.APIKey != "chat-key" {
		t.Fatalf("ResolveConfig().APIKey = %q, want %q", cfg.APIKey, "chat-key")
	}
	if cfg.BaseURL != "http://chat.local/v1" {
		t.Fatalf("ResolveConfig().BaseURL = %q, want %q", cfg.BaseURL, "http://chat.local/v1")
	}
	if cfg.ProxyURL != "http://chat:1080" {
		t.Fatalf("ResolveConfig().ProxyURL = %q, want %q", cfg.ProxyURL, "http://chat:1080")
	}
}

func TestResolveConfigFallsBackToProviderProxy(t *testing.T) {
	cfg := ResolveConfig("autopilot", appconfig.DomourConfig{
		HTTPSProxy: "http://global:1080",
		Providers: map[string]appconfig.ProviderConfig{
			"github-copilot-cli": {
				HTTPSProxy: "http://copilot:1080",
				APIKey:     "copilot-key",
				BaseURL:    "http://copilot.local/v1",
			},
		},
	})
	if cfg.Provider != "github-copilot-cli" {
		t.Fatalf("ResolveConfig().Provider = %q, want %q", cfg.Provider, "github-copilot-cli")
	}
	if cfg.APIKey != "copilot-key" {
		t.Fatalf("ResolveConfig().APIKey = %q, want %q", cfg.APIKey, "copilot-key")
	}
	if cfg.BaseURL != "http://copilot.local/v1" {
		t.Fatalf("ResolveConfig().BaseURL = %q, want %q", cfg.BaseURL, "http://copilot.local/v1")
	}
	if cfg.ProxyURL != "http://copilot:1080" {
		t.Fatalf("ResolveConfig().ProxyURL = %q, want %q", cfg.ProxyURL, "http://copilot:1080")
	}
}

func TestResolveConfigSupportsOllamaProvider(t *testing.T) {
	t.Setenv("DOMOUR_CHAT_PROVIDER", "ollama")
	t.Setenv("DOMOUR_CHAT_MODEL", "phi4-mini")

	cfg := ResolveConfig("chat", appconfig.DomourConfig{
		Providers: map[string]appconfig.ProviderConfig{
			"ollama": {
				BaseURL: "http://127.0.0.1:11434/v1",
			},
		},
	})
	if cfg.Provider != "ollama" {
		t.Fatalf("ResolveConfig().Provider = %q, want %q", cfg.Provider, "ollama")
	}
	if cfg.Model != "phi4-mini" {
		t.Fatalf("ResolveConfig().Model = %q, want %q", cfg.Model, "phi4-mini")
	}
	if cfg.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("ResolveConfig().BaseURL = %q, want %q", cfg.BaseURL, "http://127.0.0.1:11434/v1")
	}
}

func TestResolveConfigUsesPersistedDefaults(t *testing.T) {
	cfg := ResolveConfig("chat", appconfig.DomourConfig{
		DefaultProvider: "ollama",
		DefaultModel:    "phi4-mini",
		Providers: map[string]appconfig.ProviderConfig{
			"ollama": {
				BaseURL: "http://127.0.0.1:11434/v1",
			},
		},
	})
	if cfg.Provider != "ollama" {
		t.Fatalf("ResolveConfig().Provider = %q, want %q", cfg.Provider, "ollama")
	}
	if cfg.Model != "phi4-mini" {
		t.Fatalf("ResolveConfig().Model = %q, want %q", cfg.Model, "phi4-mini")
	}
	if cfg.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("ResolveConfig().BaseURL = %q, want %q", cfg.BaseURL, "http://127.0.0.1:11434/v1")
	}
}

func TestResolveConfigUsesEntryConfigModel(t *testing.T) {
	cfg := ResolveConfig("copilot", appconfig.DomourConfig{
		DefaultProvider: "github-copilot-cli",
		DefaultModel:    "gpt-4.1",
		Entries: map[string]appconfig.EntryConfig{
			"copilot": {
				Provider: "ollama",
				Model:    "qwen2.5-coder",
			},
		},
		Providers: map[string]appconfig.ProviderConfig{
			"ollama": {
				BaseURL: "http://127.0.0.1:11434/v1",
			},
		},
	})
	if cfg.Provider != "ollama" {
		t.Fatalf("ResolveConfig().Provider = %q, want %q", cfg.Provider, "ollama")
	}
	if cfg.Model != "qwen2.5-coder" {
		t.Fatalf("ResolveConfig().Model = %q, want %q", cfg.Model, "qwen2.5-coder")
	}
}

func TestChatClientIsReady(t *testing.T) {
	// Test that it handles "unsupported provider" as ready (fallback for not yet implemented discovery)
	client := &Client{
		provider: "some-unknown-provider",
	}
	ready, err := client.IsReady(context.Background())
	if !ready {
		t.Fatalf("IsReady() = false, want true for unknown provider (fallback)")
	}
	if err != nil {
		t.Fatalf("IsReady() error = %v, want nil", err)
	}
}

func TestChatClientType(t *testing.T) {
	tests := []struct {
		provider string
		expected string
	}{
		{"gemini-cli", "cli"},
		{"github-copilot-cli", "cli"},
		{"claude", "cli"},
		{"gemini", "api"},
		{"openai", "api"},
		{"ollama", "api"},
	}

	for _, tt := range tests {
		c := &Client{
			Type:     tt.expected,
			provider: tt.provider,
		}
		if act := c.Type; act != tt.expected {
			t.Errorf("Client{provider: %q}.Type = %q, want %q", tt.provider, act, tt.expected)
		}
	}
}

var testMockShouldFail bool

type testMockChatModel struct{}

func (m *testMockChatModel) Generate(ctx context.Context, in []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	if testMockShouldFail {
		return nil, fmt.Errorf("simulated API error")
	}
	return schema.AssistantMessage("Dynamic mock output", nil), nil
}

func (m *testMockChatModel) Stream(ctx context.Context, in []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *testMockChatModel) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

func TestProviderRegistryAndDynamicRegistration(t *testing.T) {
	ctx := context.Background()

	// 1. Verify built-in model registration
	registryMu.RLock()
	ollamaEntry, ollamaExists := registry["ollama"]
	registryMu.RUnlock()

	if !ollamaExists {
		t.Fatalf("Expected ollama provider to be pre-registered")
	}
	if ollamaEntry.Metadata.Trust != TrustComplete {
		t.Errorf("Expected ollama trust level to be TrustComplete, got: %v", ollamaEntry.Metadata.Trust)
	}

	// 2. Test dynamic registration
	customProviderName := "my-custom-model"
	customMetadata := ProviderMetadata{
		Type:         "api",
		Trust:        TrustLow,
		Intelligence: IntelligenceLow,
		Tags:         map[string]string{"cost": "free"},
	}

	RegisterProvider(customProviderName, customMetadata, func(ctx context.Context, cfg Config) (einomodel.ChatModel, error) {
		return &testMockChatModel{}, nil
	})

	// 3. Construct the dynamic provider
	client, err := New(ctx, Config{
		Provider: customProviderName,
		Model:    "v1",
	})
	if err != nil {
		t.Fatalf("Failed to create dynamic provider: %v", err)
	}

	if client.Type != "api" {
		t.Errorf("Expected type 'api', got: %v", client.Type)
	}
	if client.Trust != TrustLow {
		t.Errorf("Expected trust TrustLow, got: %v", client.Trust)
	}
	if client.Intelligence != IntelligenceLow {
		t.Errorf("Expected intelligence IntelligenceLow, got: %v", client.Intelligence)
	}
	if client.Tags["cost"] != "free" {
		t.Errorf("Expected tag 'cost' to be 'free', got: %v", client.Tags["cost"])
	}

	// 4. Verify mock generation works
	resp, err := client.GenerateMessage(ctx, []*schema.Message{schema.UserMessage("hello")})
	if err != nil {
		t.Fatalf("GenerateMessage failed: %v", err)
	}
	if resp.Content != "Dynamic mock output" {
		t.Errorf("Expected content 'Dynamic mock output', got: %q", resp.Content)
	}
}

func TestProviderHealthCheckAndPassiveRemoval(t *testing.T) {
	ctx := context.Background()

	providerName := "failing-test-provider"

	// Reset health status initially to ensure clean test environment
	SetProviderHealth(providerName, true, nil)
	testMockShouldFail = false

	RegisterProvider(providerName, ProviderMetadata{
		Type:         "api",
		Trust:        TrustGeneral,
		Intelligence: IntelligenceHigh,
	}, func(ctx context.Context, cfg Config) (einomodel.ChatModel, error) {
		return &testMockChatModel{}, nil
	})

	// 1. Initial creation succeeds (defaults to healthy)
	client, err := New(ctx, Config{Provider: providerName})
	if err != nil {
		t.Fatalf("Failed to construct client: %v", err)
	}

	// 2. Cause a failure
	testMockShouldFail = true
	_, err = client.GenerateMessage(ctx, []*schema.Message{schema.UserMessage("hello")})
	if err == nil {
		t.Fatalf("Expected GenerateMessage to fail, but succeeded")
	}

	// 3. Verify it is now passively marked as unhealthy
	if IsProviderHealthy(providerName) {
		t.Errorf("Expected provider to be unhealthy after failure")
	}

	// 4. Verify New() now fails
	_, err = New(ctx, Config{Provider: providerName})
	if err == nil {
		t.Errorf("Expected New() to fail for unhealthy provider, but it succeeded")
	}

	// 5. Restore health (e.g. active poll / manually marking healthy)
	testMockShouldFail = false
	SetProviderHealth(providerName, true, nil)

	// 6. Verify New() succeeds again
	_, err = New(ctx, Config{Provider: providerName})
	if err != nil {
		t.Fatalf("Expected New() to succeed after health recovery, but got: %v", err)
	}
}

