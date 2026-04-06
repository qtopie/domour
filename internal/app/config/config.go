package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-viper/encoding/ini"
	"github.com/spf13/viper"
)

const DefaultHTTPSProxy = ""

type ProviderConfig struct {
	HTTPSProxy string `json:"https_proxy"`
	APIKey     string `json:"api_key,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
}

type ServiceConfig struct {
	Mode  string `json:"mode,omitempty"`
	AppID string `json:"app_id,omitempty"`
}

type DaprConfig struct {
	GRPCAddress string `json:"grpc_address,omitempty"`
	HTTPAddress string `json:"http_address,omitempty"`
}

type DomourConfig struct {
	HTTPSProxy string                    `json:"https_proxy"`
	Providers  map[string]ProviderConfig `json:"providers,omitempty"`
	Services   map[string]ServiceConfig  `json:"services,omitempty"`
	Dapr       DaprConfig                `json:"dapr,omitempty"`
}

var (
	viperCfg  *viper.Viper
	viperOnce sync.Once

	domourCfg  DomourConfig
	domourErr  error
	domourOnce sync.Once
)

func GetAppConfig() *viper.Viper {
	viperOnce.Do(initLegacyConfig)
	return viperCfg
}

func LoadDomourConfig() (DomourConfig, error) {
	domourOnce.Do(func() {
		path, err := DomourConfigPath()
		if err != nil {
			domourErr = err
			return
		}
		domourCfg, domourErr = loadOrCreateDomourConfig(path)
	})
	return domourCfg, domourErr
}

func DomourConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(homeDir, ".domour", "config.json"), nil
}

func (c DomourConfig) ProxyForProvider(provider string) string {
	if c.Providers != nil {
		if proxy := strings.TrimSpace(c.Providers[normalizeProviderKey(provider)].HTTPSProxy); proxy != "" {
			return proxy
		}
	}
	return strings.TrimSpace(c.HTTPSProxy)
}

func (c DomourConfig) APIKeyForProvider(provider string) string {
	if c.Providers != nil {
		return strings.TrimSpace(c.Providers[normalizeProviderKey(provider)].APIKey)
	}
	return ""
}

func (c DomourConfig) BaseURLForProvider(provider string) string {
	if c.Providers != nil {
		return strings.TrimSpace(c.Providers[normalizeProviderKey(provider)].BaseURL)
	}
	return ""
}

func (c DomourConfig) ServiceMode(name string) string {
	if c.Services != nil {
		if mode := strings.TrimSpace(c.Services[strings.ToLower(strings.TrimSpace(name))].Mode); mode != "" {
			return mode
		}
	}
	return "local"
}

func (c DomourConfig) ServiceAppID(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if envKey := "DOMOUR_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_APP_ID"; name != "" {
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			return value
		}
	}
	if c.Services != nil {
		if appID := strings.TrimSpace(c.Services[name].AppID); appID != "" {
			return appID
		}
	}
	switch name {
	case "brain":
		return "domour-brain"
	case "motor":
		return "domour-motor"
	default:
		return "domour-" + name
	}
}

func (c DomourConfig) DaprGRPCAddress() string {
	if value := strings.TrimSpace(os.Getenv("DOMOUR_DAPR_GRPC_ADDRESS")); value != "" {
		return value
	}
	if value := strings.TrimSpace(c.Dapr.GRPCAddress); value != "" {
		return value
	}
	return "127.0.0.1:50001"
}

func (c DomourConfig) DaprHTTPAddress() string {
	if value := strings.TrimSpace(os.Getenv("DOMOUR_DAPR_HTTP_ADDRESS")); value != "" {
		return value
	}
	if value := strings.TrimSpace(c.Dapr.HTTPAddress); value != "" {
		return value
	}
	return "127.0.0.1:3500"
}

func loadOrCreateDomourConfig(path string) (DomourConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return DomourConfig{}, fmt.Errorf("read domour config %s: %w", path, err)
		}

		cfg := defaultDomourConfig()
		if err := writeDomourConfig(path, cfg); err != nil {
			return DomourConfig{}, err
		}
		return cfg, nil
	}

	var cfg DomourConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DomourConfig{}, fmt.Errorf("decode domour config %s: %w", path, err)
	}

	if strings.TrimSpace(cfg.HTTPSProxy) == "" {
		cfg.HTTPSProxy = DefaultHTTPSProxy
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	if cfg.Services == nil {
		cfg.Services = map[string]ServiceConfig{}
	}
	return cfg, nil
}

func writeDomourConfig(path string, cfg DomourConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create domour config dir: %w", err)
	}

	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode domour config: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write domour config %s: %w", path, err)
	}
	return nil
}

func defaultDomourConfig() DomourConfig {
	return DomourConfig{
		HTTPSProxy: DefaultHTTPSProxy,
		Providers: map[string]ProviderConfig{
			"gemini": {
				HTTPSProxy: DefaultHTTPSProxy,
			},
		},
		Services: map[string]ServiceConfig{
			"brain": {Mode: "local", AppID: "domour-brain"},
			"motor": {Mode: "local", AppID: "domour-motor"},
		},
		Dapr: DaprConfig{
			GRPCAddress: "127.0.0.1:50001",
			HTTPAddress: "127.0.0.1:3500",
		},
	}
}

func initLegacyConfig() {
	codecRegistry := viper.NewCodecRegistry()
	codecRegistry.RegisterCodec("ini", ini.Codec{})

	viperCfg = viper.NewWithOptions(
		viper.WithCodecRegistry(codecRegistry),
	)

	viperCfg.SetConfigName("config")
	viperCfg.SetConfigType("ini")
	viperCfg.AddConfigPath(".")

	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		viperCfg.AddConfigPath(filepath.Join(homeDir, ".cosmos"))
	}

	if err := viperCfg.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			log.Printf("Error reading legacy config file: %v", err)
		}
	}
}

func normalizeProviderKey(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gemini", "gemini-cli", "gemini_cli":
		return "gemini"
	case "ollama":
		return "ollama"
	case "github-copilot-cli", "copilot-cli", "github-copilot":
		return "github-copilot-cli"
	case "qodercli", "qoder-cli", "qoder":
		return "qodercli"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}
