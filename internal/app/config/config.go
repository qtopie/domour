package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/go-viper/encoding/ini"
	"github.com/spf13/viper"
)

const DefaultHTTPSProxy = ""

type ProviderConfig struct {
	HTTPSProxy string   `json:"https_proxy"`
	APIKey     string   `json:"api_key,omitempty"`
	BaseURL    string   `json:"base_url,omitempty"`
	Model      string   `json:"model,omitempty"`
	Models     []string `json:"models,omitempty"`
}

type EntryConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
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
	HTTPSProxy      string                    `json:"https_proxy"`
	LogAsJSON       bool                      `json:"log_as_json,omitempty"`
	DefaultProvider string                    `json:"default_provider,omitempty"`
	DefaultModel    string                    `json:"default_model,omitempty"`
	Providers       map[string]ProviderConfig `json:"providers,omitempty"`
	Entries         map[string]EntryConfig    `json:"entries,omitempty"`
	Services        map[string]ServiceConfig  `json:"services,omitempty"`
	Dapr            DaprConfig                `json:"dapr,omitempty"`
}

var (
	viperCfg  *viper.Viper
	viperOnce sync.Once

	domourCfg  DomourConfig
	domourErr  error
	domourOnce sync.Once
	domourMu   sync.Mutex
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

	domourMu.Lock()
	defer domourMu.Unlock()
	return domourCfg, domourErr
}
func ReloadDomourConfig() (DomourConfig, error) {
	path, err := DomourConfigPath()
	if err != nil {
		return DomourConfig{}, err
	}
	cfg, err := loadOrCreateDomourConfig(path)
	if err != nil {
		return DomourConfig{}, err
	}

	domourMu.Lock()
	defer domourMu.Unlock()
	domourCfg = cfg
	domourErr = nil
	return domourCfg, nil
}


func SaveDomourConfig(cfg DomourConfig) error {
	path, err := DomourConfigPath()
	if err != nil {
		return err
	}
	return SaveDomourConfigAt(path, cfg)
}

func SaveDomourConfigAt(path string, cfg DomourConfig) error {
	cfg = normalizeDomourConfig(cfg)
	if err := writeDomourConfig(path, cfg); err != nil {
		return err
	}

	domourMu.Lock()
	defer domourMu.Unlock()
	domourCfg = cfg
	domourErr = nil
	return nil
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

func (c DomourConfig) ProviderModel(provider string) string {
	if c.Providers != nil {
		return strings.TrimSpace(c.Providers[normalizeProviderKey(provider)].Model)
	}
	return ""
}

func (c DomourConfig) ProviderModels(provider string) []string {
	if c.Providers == nil {
		return nil
	}
	return append([]string(nil), c.Providers[normalizeProviderKey(provider)].Models...)
}

func (c DomourConfig) DefaultProviderName() string {
	return normalizeProviderKey(c.DefaultProvider)
}

func (c DomourConfig) DefaultModelName() string {
	return strings.TrimSpace(c.DefaultModel)
}

func (c DomourConfig) EntryProvider(name string) string {
	if c.Entries == nil {
		return ""
	}
	return normalizeProviderKey(c.Entries[normalizeEntryKey(name)].Provider)
}

func (c DomourConfig) EntryModel(name string) string {
	if c.Entries == nil {
		return ""
	}
	return strings.TrimSpace(c.Entries[normalizeEntryKey(name)].Model)
}

func (c *DomourConfig) SetDefaultSelection(provider, model string) {
	if c == nil {
		return
	}
	provider = normalizeProviderKey(provider)
	model = strings.TrimSpace(model)
	if provider != "" {
		c.DefaultProvider = provider
	}
	c.DefaultModel = model
	if provider != "" && model != "" {
		c.SetProviderModel(provider, model)
	}
}

func (c *DomourConfig) SetEntrySelection(entry, provider, model string) {
	if c == nil {
		return
	}
	entry = normalizeEntryKey(entry)
	if entry == "" {
		c.SetDefaultSelection(provider, model)
		return
	}
	if c.Entries == nil {
		c.Entries = map[string]EntryConfig{}
	}
	cfg := c.Entries[entry]
	if normalizedProvider := normalizeProviderKey(provider); normalizedProvider != "" {
		cfg.Provider = normalizedProvider
		if strings.TrimSpace(model) != "" {
			c.SetProviderModel(normalizedProvider, model)
		}
	}
	cfg.Model = strings.TrimSpace(model)
	c.Entries[entry] = cfg
}

func (c *DomourConfig) SetProviderModel(provider, model string) {
	if c == nil {
		return
	}
	provider = normalizeProviderKey(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		return
	}
	if c.Providers == nil {
		c.Providers = map[string]ProviderConfig{}
	}
	cfg := c.Providers[provider]
	cfg.Model = model
	c.Providers[provider] = cfg
}

func (c *DomourConfig) SetProviderDiscoveredModels(provider string, models []string) {
	if c == nil {
		return
	}
	provider = normalizeProviderKey(provider)
	if provider == "" {
		return
	}
	if c.Providers == nil {
		c.Providers = map[string]ProviderConfig{}
	}
	cfg := c.Providers[provider]
	cfg.Models = normalizeAndDeduplicateModelIDs(models)
	c.Providers[provider] = cfg
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

func (c DomourConfig) IsLogAsJSON() bool {
	if value := strings.ToLower(strings.TrimSpace(os.Getenv("DOMOUR_LOG_AS_JSON"))); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return c.LogAsJSON
}

func loadOrCreateDomourConfig(path string) (DomourConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return DomourConfig{}, fmt.Errorf("read domour config %s: %w", path, err)
		}

		cfg := normalizeDomourConfig(defaultDomourConfig())
		if err := writeDomourConfig(path, cfg); err != nil {
			return DomourConfig{}, err
		}
		return cfg, nil
	}

	var cfg DomourConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DomourConfig{}, fmt.Errorf("decode domour config %s: %w", path, err)
	}

	return normalizeDomourConfig(cfg), nil
}

func writeDomourConfig(path string, cfg DomourConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create domour config dir: %w", err)
	}

	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode domour config: %w", err)
	}
	content = append(content, 
)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write domour config %s: %w", path, err)
	}
	return nil
}

func defaultDomourConfig() DomourConfig {
	return DomourConfig{
		HTTPSProxy:      DefaultHTTPSProxy,
		DefaultProvider: "github-copilot-cli",
		Providers: map[string]ProviderConfig{
			"gemini": {
				HTTPSProxy: DefaultHTTPSProxy,
			},
		},
		Entries: map[string]EntryConfig{},
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

func normalizeDomourConfig(cfg DomourConfig) DomourConfig {
	cfg.HTTPSProxy = strings.TrimSpace(cfg.HTTPSProxy)
	if cfg.HTTPSProxy == "" {
		cfg.HTTPSProxy = DefaultHTTPSProxy
	}
	cfg.DefaultProvider = normalizeProviderKey(cfg.DefaultProvider)
	cfg.DefaultModel = strings.TrimSpace(cfg.DefaultModel)

	normalizedProviders := make(map[string]ProviderConfig, len(cfg.Providers))
	for key, providerCfg := range cfg.Providers {
		normalizedKey := normalizeProviderKey(key)
		if normalizedKey == "" {
			continue
		}
		providerCfg.HTTPSProxy = strings.TrimSpace(providerCfg.HTTPSProxy)
		providerCfg.APIKey = strings.TrimSpace(providerCfg.APIKey)
		providerCfg.BaseURL = strings.TrimSpace(providerCfg.BaseURL)
		providerCfg.Model = strings.TrimSpace(providerCfg.Model)
		providerCfg.Models = normalizeAndDeduplicateModelIDs(providerCfg.Models)
		normalizedProviders[normalizedKey] = providerCfg
	}
	cfg.Providers = normalizedProviders
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}

	normalizedEntries := make(map[string]EntryConfig, len(cfg.Entries))
	for key, entryCfg := range cfg.Entries {
		normalizedKey := normalizeEntryKey(key)
		if normalizedKey == "" {
			continue
		}
		entryCfg.Provider = normalizeProviderKey(entryCfg.Provider)
		entryCfg.Model = strings.TrimSpace(entryCfg.Model)
		normalizedEntries[normalizedKey] = entryCfg
	}
	cfg.Entries = normalizedEntries
	if cfg.Entries == nil {
		cfg.Entries = map[string]EntryConfig{}
	}

	if cfg.Services == nil {
		cfg.Services = map[string]ServiceConfig{}
	}

	return cfg
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
	case "gemini":
		return "gemini"
	case "gemini-cli", "gemini_cli":
		return "gemini-cli"
	case "agy-sdk", "agy_sdk", "antigravity-sdk":
		return "agy-sdk"
	case "agy-cli", "agy_cli", "agy":
		return "agy-cli"
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

func normalizeEntryKey(entry string) string {
	return strings.ToLower(strings.TrimSpace(entry))
}

func normalizeAndDeduplicateModelIDs(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	sort.Strings(out)
	return out
}