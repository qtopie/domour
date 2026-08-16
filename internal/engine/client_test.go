package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/qtopie/domour/internal/cognitor/proxy"
	appconfig "github.com/qtopie/domour/internal/config"
)

// testConfig builds a DomourConfig matching the spec's given conditions.
func testConfig() appconfig.DomourConfig {
	return appconfig.DomourConfig{
		DefaultProvider: "llamacpp",
		DefaultModel:    "gemma-2-2b",
		Providers: map[string]appconfig.ProviderConfig{
			"llamacpp": {Enabled: true, BaseURL: "http://127.0.0.1:11434/v1", Model: "gemma-2-2b"},
			"deepseek": {Enabled: true, APIKey: "sk-test", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"},
			"openai":   {Enabled: true}, // enabled but no api_key/base_url
			"claude":   {},              // not enabled
		},
		Entries: map[string]appconfig.EntryConfig{
			"chat": {Provider: "llamacpp", Model: "gemma-2-2b"},
		},
	}
}

// fakeBuild returns a client for the given provider/model, or an error if
// failProviders contains the provider.
func fakeBuild(failProviders ...string) buildClientFunc {
	return func(_ context.Context, _ string, provider, model string) (*proxy.Client, error) {
		for _, f := range failProviders {
			if strings.EqualFold(f, provider) {
				return nil, errors.New("provider " + provider + " is unhealthy: connection refused")
			}
		}
		return proxy.NewTestClient(provider, model, nil), nil
	}
}

// fakeReady reports readiness per provider.
func fakeReady(readyProviders ...string) readyFunc {
	return func(_ context.Context, c *proxy.Client) (bool, error) {
		for _, p := range readyProviders {
			if strings.EqualFold(p, c.Provider()) {
				return true, nil
			}
		}
		return false, errors.New("provider " + c.Provider() + " is not ready")
	}
}

// [SPEC-AUTOFB-001] Primary provider healthy → used directly, no fallback.
func TestGetClientWithOverride_PrimaryHealthy(t *testing.T) {
	cfg := testConfig()

	client, err := resolveWithFallback(context.Background(), "chat", "llamacpp", "gemma-2-2b", cfg,
		fakeBuild(), fakeReady("llamacpp"))

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if client.Provider() != "llamacpp" {
		t.Errorf("expected provider llamacpp, got %q", client.Provider())
	}
	if client.Model() != "gemma-2-2b" {
		t.Errorf("expected model gemma-2-2b, got %q", client.Model())
	}
}

// [SPEC-AUTOFB-002] Primary provider unhealthy → automatic fallback to enabled provider.
func TestGetClientWithOverride_FallbackToEnabledProvider(t *testing.T) {
	cfg := testConfig()

	client, err := resolveWithFallback(context.Background(), "chat", "llamacpp", "gemma-2-2b", cfg,
		fakeBuild("llamacpp"), fakeReady("deepseek"))

	if err != nil {
		t.Fatalf("expected fallback success, got: %v", err)
	}
	if client.Provider() != "deepseek" {
		t.Errorf("expected fallback provider deepseek, got %q", client.Provider())
	}
}

// [SPEC-AUTOFB-003] Primary unhealthy, fallback candidate fails readiness too → original error returned.
func TestGetClientWithOverride_AllFallbacksFail(t *testing.T) {
	cfg := testConfig()

	_, err := resolveWithFallback(context.Background(), "chat", "llamacpp", "gemma-2-2b", cfg,
		fakeBuild("llamacpp"), fakeReady()) // nothing ready

	if err == nil {
		t.Fatal("expected error when all fallbacks fail")
	}
	// Must return the original primary error.
	if !strings.Contains(err.Error(), "llamacpp is unhealthy") {
		t.Errorf("expected original primary error, got: %v", err)
	}
}

// [SPEC-AUTOFB-004] Disabled / unconfigured providers never selected as fallback.
func TestGetClientWithOverride_SkipsDisabledOrUnconfigured(t *testing.T) {
	cfg := testConfig()
	cfg.Providers["ollama"] = appconfig.ProviderConfig{Enabled: true, BaseURL: "http://127.0.0.1:11434/v1", Model: "phi4"}

	cands := fallbackCandidates("chat", "llamacpp", "gemma-2-2b", cfg)

	// openai (no key/url) and claude (disabled) must never appear.
	for _, c := range cands {
		if c.provider == "openai" || c.provider == "claude" {
			t.Errorf("candidate %q should never be selected (disabled/unconfigured)", c.provider)
		}
	}
	// deepseek (enabled+configured) must be present.
	found := false
	for _, c := range cands {
		if c.provider == "deepseek" {
			found = true
		}
	}
	if !found {
		t.Error("expected deepseek in fallback candidates")
	}
}

// [SPEC-AUTOFB-005] Entry provider differs from requested → entry provider tried before defaults.
func TestGetClientWithOverride_EntryProviderPriority(t *testing.T) {
	cfg := testConfig()
	cfg.Entries["chat"] = appconfig.EntryConfig{Provider: "deepseek", Model: "deepseek-v4-flash"}

	// request gemini; entry chat uses deepseek; default is llamacpp.
	client, err := resolveWithFallback(context.Background(), "chat", "gemini", "", cfg,
		fakeBuild("gemini"), fakeReady("deepseek", "llamacpp"))

	if err != nil {
		t.Fatalf("expected fallback success, got: %v", err)
	}
	if client.Provider() != "deepseek" {
		t.Errorf("expected entry provider deepseek (priority #2), got %q", client.Provider())
	}
}

// Candidate priority ordering verification.
func TestFallbackCandidates_PriorityOrder(t *testing.T) {
	cfg := testConfig()
	cfg.Entries["chat"] = appconfig.EntryConfig{Provider: "deepseek", Model: "deepseek-v4-flash"}

	cands := fallbackCandidates("chat", "gemini", "", cfg)

	got := make([]string, 0, len(cands))
	for _, c := range cands {
		got = append(got, c.provider)
	}

	// gemini (requested) → deepseek (entry) → llamacpp (default)
	expect := []string{"gemini", "deepseek", "llamacpp"}
	if len(got) < len(expect) {
		t.Fatalf("expected at least %v candidates, got %v", expect, got)
	}
	for i := range expect {
		if got[i] != expect[i] {
			t.Errorf("candidate[%d] = %q, want %q (full: %v)", i, got[i], expect[i], got)
		}
	}
}

// Duplicate providers are never repeated in the candidate list.
func TestFallbackCandidates_DeduplicatesPrimary(t *testing.T) {
	cfg := testConfig()

	// Requested provider == entry provider == default provider → must appear once.
	cands := fallbackCandidates("chat", "llamacpp", "gemma-2-2b", cfg)

	count := 0
	for _, c := range cands {
		if c.provider == "llamacpp" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected llamacpp exactly once, got %d times: %+v", count, cands)
	}
}

// Lexicographic ordering for remaining enabled+configured providers.
func TestFallbackCandidates_LexicographicTail(t *testing.T) {
	cfg := testConfig()
	cfg.Providers["gemma"] = appconfig.ProviderConfig{Enabled: true, APIKey: "sk-a", Model: "g"}
	cfg.Providers["zephyr"] = appconfig.ProviderConfig{Enabled: true, APIKey: "sk-z", Model: "z"}

	cands := fallbackCandidates("chat", "llamacpp", "gemma-2-2b", cfg)

	// Tail after llamacpp should be deepseek, gemma, zephyr (lexicographic), all before ollama? no ollama.
	tail := []string{}
	for _, c := range cands {
		if c.provider != "llamacpp" {
			tail = append(tail, c.provider)
		}
	}
	want := []string{"deepseek", "gemma", "zephyr"}
	for i := range want {
		if i >= len(tail) || tail[i] != want[i] {
			t.Errorf("tail[%d] = %v, want %v (full tail: %v)", i, tail, want, gotSafe(tail, i))
			break
		}
	}
}

func gotSafe(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "<missing>"
}
