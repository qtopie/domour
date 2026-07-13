package config

import (
	"path/filepath"
	"testing"
)

func TestLoadOrCreateDomourConfigCreatesDefault(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := loadOrCreateDomourConfig(path)
	if err != nil {
		t.Fatalf("loadOrCreateDomourConfig() error = %v", err)
	}

	if got := cfg.HTTPSProxy; got != DefaultHTTPSProxy {
		t.Fatalf("HTTPSProxy = %q, want %q", got, DefaultHTTPSProxy)
	}

	if got := cfg.ProxyForProvider("gemini-cli"); got != DefaultHTTPSProxy {
		t.Fatalf("ProxyForProvider(gemini-cli) = %q, want %q", got, DefaultHTTPSProxy)
	}
	if got := cfg.DefaultProviderName(); got != "github-copilot-cli" {
		t.Fatalf("DefaultProviderName() = %q, want %q", got, "github-copilot-cli")
	}
	if got := cfg.ServiceAppID("brain"); got != "domour-brain" {
		t.Fatalf("ServiceAppID(brain) = %q, want %q", got, "domour-brain")
	}
	if got := cfg.DaprHTTPAddress(); got != "127.0.0.1:3500" {
		t.Fatalf("DaprHTTPAddress() = %q, want %q", got, "127.0.0.1:3500")
	}
}

func TestProxyForProviderPrefersProviderOverride(t *testing.T) {
	t.Parallel()

	cfg := DomourConfig{
		HTTPSProxy: "http://global:1080",
		Providers: map[string]ProviderConfig{
			"github-copilot-cli": {
				HTTPSProxy: "http://copilot:1080",
			},
		},
	}

	if got := cfg.ProxyForProvider("copilot-cli"); got != "http://copilot:1080" {
		t.Fatalf("ProxyForProvider(copilot-cli) = %q, want %q", got, "http://copilot:1080")
	}
	if got := cfg.ProxyForProvider("gemini-cli"); got != "http://global:1080" {
		t.Fatalf("ProxyForProvider(gemini-cli) = %q, want %q", got, "http://global:1080")
	}
}

func TestProviderConfigNormalizesLlamaCpp(t *testing.T) {
	t.Parallel()

	cfg := DomourConfig{
		Providers: map[string]ProviderConfig{
			"llamacpp": {
				BaseURL: "http://127.0.0.1:8080/v1",
				APIKey:  "llamacpp",
			},
		},
	}

	if got := cfg.BaseURLForProvider("llamacpp"); got != "http://127.0.0.1:8080/v1" {
		t.Fatalf("BaseURLForProvider(llamacpp) = %q, want %q", got, "http://127.0.0.1:8080/v1")
	}
	if got := cfg.APIKeyForProvider("llamacpp"); got != "llamacpp" {
		t.Fatalf("APIKeyForProvider(llamacpp) = %q, want %q", got, "llamacpp")
	}
}

func TestServiceAppIDPrefersOverride(t *testing.T) {
	t.Parallel()

	cfg := DomourConfig{
		Services: map[string]ServiceConfig{
			"brain": {
				AppID: "custom-brain",
			},
		},
		Dapr: DaprConfig{
			HTTPAddress: "127.0.0.1:3600",
		},
	}

	if got := cfg.ServiceAppID("brain"); got != "custom-brain" {
		t.Fatalf("ServiceAppID(brain) = %q, want %q", got, "custom-brain")
	}
	if got := cfg.ServiceAppID("motor"); got != "domour-motor" {
		t.Fatalf("ServiceAppID(motor) = %q, want %q", got, "domour-motor")
	}
	if got := cfg.DaprHTTPAddress(); got != "127.0.0.1:3600" {
		t.Fatalf("DaprHTTPAddress() = %q, want %q", got, "127.0.0.1:3600")
	}
}

func TestSetDefaultAndEntrySelection(t *testing.T) {
	t.Parallel()

	cfg := DomourConfig{}
	cfg.SetDefaultSelection("copilot-cli", "gpt-5")
	cfg.SetEntrySelection("chat", "llamacpp", "phi4-mini")
	cfg.SetProviderDiscoveredModels("llamacpp", []string{"phi4-mini", "llama3.2", "phi4-mini"})

	if got := cfg.DefaultProviderName(); got != "github-copilot-cli" {
		t.Fatalf("DefaultProviderName() = %q, want %q", got, "github-copilot-cli")
	}
	if got := cfg.DefaultModelName(); got != "gpt-5" {
		t.Fatalf("DefaultModelName() = %q, want %q", got, "gpt-5")
	}
	if got := cfg.EntryProvider("chat"); got != "llamacpp" {
		t.Fatalf("EntryProvider(chat) = %q, want %q", got, "llamacpp")
	}
	if got := cfg.EntryModel("chat"); got != "phi4-mini" {
		t.Fatalf("EntryModel(chat) = %q, want %q", got, "phi4-mini")
	}
	gotModels := cfg.ProviderModels("llamacpp")
	if len(gotModels) != 2 || gotModels[0] != "llama3.2" || gotModels[1] != "phi4-mini" {
		t.Fatalf("ProviderModels(llamacpp) = %#v, want sorted unique models", gotModels)
	}
}

func TestDaprAddressAndAppIDPreferEnv(t *testing.T) {
	t.Setenv("DOMOUR_DAPR_HTTP_ADDRESS", "127.0.0.1:9999")
	t.Setenv("DOMOUR_DAPR_GRPC_ADDRESS", "127.0.0.1:9998")
	t.Setenv("DOMOUR_BRAIN_APP_ID", "brain-from-env")

	cfg := DomourConfig{
		Services: map[string]ServiceConfig{
			"brain": {AppID: "brain-from-config"},
		},
		Dapr: DaprConfig{
			HTTPAddress: "127.0.0.1:3500",
			GRPCAddress: "127.0.0.1:50001",
		},
	}

	if got := cfg.DaprHTTPAddress(); got != "127.0.0.1:9999" {
		t.Fatalf("DaprHTTPAddress() = %q, want %q", got, "127.0.0.1:9999")
	}
	if got := cfg.DaprGRPCAddress(); got != "127.0.0.1:9998" {
		t.Fatalf("DaprGRPCAddress() = %q, want %q", got, "127.0.0.1:9998")
	}
	if got := cfg.ServiceAppID("brain"); got != "brain-from-env" {
		t.Fatalf("ServiceAppID(brain) = %q, want %q", got, "brain-from-env")
	}
}

func TestIsDebug(t *testing.T) {
	cfg := DomourConfig{}
	
	t.Run("DefaultFalse", func(t *testing.T) {
		if cfg.IsDebug() {
			t.Error("IsDebug() should be false by default")
		}
	})

	t.Run("EnvTrue", func(t *testing.T) {
		t.Setenv("DOMOUR_DEBUG", "true")
		if !cfg.IsDebug() {
			t.Error("IsDebug() should be true when DOMOUR_DEBUG=true")
		}
	})

	t.Run("Env1", func(t *testing.T) {
		t.Setenv("DOMOUR_DEBUG", "1")
		if !cfg.IsDebug() {
			t.Error("IsDebug() should be true when DOMOUR_DEBUG=1")
		}
	})

	t.Run("EnvYes", func(t *testing.T) {
		t.Setenv("DOMOUR_DEBUG", "yes")
		if !cfg.IsDebug() {
			t.Error("IsDebug() should be true when DOMOUR_DEBUG=yes")
		}
	})

	t.Run("EnvFalse", func(t *testing.T) {
		t.Setenv("DOMOUR_DEBUG", "false")
		if cfg.IsDebug() {
			t.Error("IsDebug() should be false when DOMOUR_DEBUG=false")
		}
	})
}

func TestDomourHomeDirOverrides(t *testing.T) {
	t.Run("DOMOUR_HOME env override", func(t *testing.T) {
		t.Setenv("DOMOUR_HOME", "/custom/domour/home")
		if got := DomourHomeDir(); got != "/custom/domour/home" {
			t.Errorf("DomourHomeDir() = %q, want %q", got, "/custom/domour/home")
		}
	})

	t.Run("COSMOS_HOME env override", func(t *testing.T) {
		t.Setenv("DOMOUR_HOME", "")
		t.Setenv("COSMOS_HOME", "/custom/cosmos/home")
		if got := DomourHomeDir(); got != "/custom/cosmos/home" {
			t.Errorf("DomourHomeDir() = %q, want %q", got, "/custom/cosmos/home")
		}
	})

	t.Run("COSMOS_STAR_HOME env override", func(t *testing.T) {
		t.Setenv("DOMOUR_HOME", "")
		t.Setenv("COSMOS_HOME", "")
		t.Setenv("COSMOS_STAR_HOME", "/custom/cosmos-star/home")
		if got := DomourHomeDir(); got != "/custom/cosmos-star/home" {
			t.Errorf("DomourHomeDir() = %q, want %q", got, "/custom/cosmos-star/home")
		}
	})
}
