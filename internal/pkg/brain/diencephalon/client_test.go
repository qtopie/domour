package diencephalon

import (
	"testing"

	appconfig "github.com/qtopie/domour/internal/app/config"
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
