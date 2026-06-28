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
	HTTPSProxy            string   `json:"https_proxy"`
	APIKey                string   `json:"api_key,omitempty"`
	BaseURL               string   `json:"base_url,omitempty"`
	Model                 string   `json:"model,omitempty"`
	Models                []string `json:"models,omitempty"`
	MaxActiveTokens       int      `json:"max_active_tokens,omitempty"`
	CompressTriggerTokens int      `json:"compress_trigger_tokens,omitempty"`
	Enabled               bool     `json:"enabled,omitempty"`
}

type EntryConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// ModeMapping defines tag-based model selection rules for a system mode.
type ModeMapping struct {
	Require []string `json:"require,omitempty"` // All tags must be present
	Prefer  []string `json:"prefer,omitempty"`  // Score by these tags (order = priority chain)
	Exclude []string `json:"exclude,omitempty"` // Models with these tags are excluded
}

type ServiceConfig struct {
	Mode  string `json:"mode,omitempty"`
	AppID string `json:"app_id,omitempty"`
}

type DaprConfig struct {
	GRPCAddress string `json:"grpc_address,omitempty"`
	HTTPAddress string `json:"http_address,omitempty"`
}

type TelemetryConfig struct {
	ServiceName    string  `json:"service_name,omitempty"`
	ServiceVersion string  `json:"service_version,omitempty"`
	Endpoint       string  `json:"endpoint,omitempty"`
	Sampler        float64 `json:"sampler,omitempty"`
	UseStdout      bool    `json:"use_stdout,omitempty"`
}

type MCPServerConfig struct {
	Type    string            `json:"type"` // "stdio", "sse", "dapr"
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	AppID   string            `json:"app_id,omitempty"`
}

type DomourConfig struct {
	HTTPSProxy            string                     `json:"https_proxy"`
	LogAsJSON             bool                       `json:"log_as_json,omitempty"`
	DefaultProvider       string                     `json:"default_provider,omitempty"`
	DefaultModel          string                     `json:"default_model,omitempty"`
	Providers             map[string]ProviderConfig  `json:"providers,omitempty"`
	Entries               map[string]EntryConfig     `json:"entries,omitempty"`
	ModeMappings          map[string]ModeMapping     `json:"mode_mappings,omitempty"`
	Services              map[string]ServiceConfig   `json:"services,omitempty"`
	Dapr                  DaprConfig                 `json:"dapr,omitempty"`
	Telemetry             TelemetryConfig            `json:"telemetry,omitempty"`
	MCPDir                string                     `json:"mcp_dir,omitempty"`
	SkillsDir             string                     `json:"skills_dir,omitempty"`
	MCPServers            map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
	MaxActiveTokens       int                        `json:"max_active_tokens,omitempty"`
	CompressTriggerTokens int                        `json:"compress_trigger_tokens,omitempty"`
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

func DomourHomeDir() string {
	if val := strings.TrimSpace(os.Getenv("DOMOUR_HOME")); val != "" {
		return val
	}
	if val := strings.TrimSpace(os.Getenv("COSMOS_HOME")); val != "" {
		return val
	}
	if val := strings.TrimSpace(os.Getenv("COSMOS_STAR_HOME")); val != "" {
		return val
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".domour"
	}

	cosmosStarDir := filepath.Join(homeDir, ".cosmos-star")
	if info, err := os.Stat(cosmosStarDir); err == nil && info.IsDir() {
		return cosmosStarDir
	}

	cosmosDir := filepath.Join(homeDir, ".cosmos")
	if info, err := os.Stat(cosmosDir); err == nil && info.IsDir() {
		return cosmosDir
	}

	return filepath.Join(homeDir, ".domour")
}

func DomourConfigPath() (string, error) {
	return filepath.Join(DomourHomeDir(), "config.json"), nil
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

// ModeMapping returns the tag rules for a given mode, falling back to defaults.
func (c DomourConfig) ModeMapping(mode string) ModeMapping {
	if c.ModeMappings != nil {
		if m, ok := c.ModeMappings[strings.ToLower(strings.TrimSpace(mode))]; ok {
			return m
		}
	}
	return defaultModeMapping(mode)
}

// defaultModeMapping returns the built-in tag rules for each system mode.
func defaultModeMapping(mode string) ModeMapping {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "deep_think":
		return ModeMapping{
			Require: []string{"deep"},
			Prefer:  []string{"flash", "pro"},
			Exclude: []string{"billed"},
		}
	case "performance":
		return ModeMapping{
			Prefer:  []string{"pro", "flash"},
			Exclude: []string{"billed"},
		}
	case "stealth":
		return ModeMapping{
			Require: []string{"local", "private"},
			Prefer:  []string{"free"},
			Exclude: []string{"billed"},
		}
	case "survival":
		return ModeMapping{
			Require: []string{"local", "free"},
			Exclude: []string{"billed", "remote"},
		}
	case "balanced":
		return ModeMapping{
			Prefer:  []string{"flash"},
			Exclude: []string{"billed"},
		}
	case "casual":
		return ModeMapping{
			Prefer:  []string{"lite", "free"},
			Exclude: []string{"billed"},
		}
	case "vigilant":
		return ModeMapping{
			Require: []string{"local"},
			Prefer:  []string{"flash", "lite"},
		}
	case "diagnostic":
		return ModeMapping{}
	case "hibernate":
		return ModeMapping{} // No LLM in hibernate
	default:
		return ModeMapping{
			Exclude: []string{"billed"},
		}
	}
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

func (c DomourConfig) TelemetryServiceName() string {
	if value := strings.TrimSpace(os.Getenv("DOMOUR_TELEMETRY_SERVICE_NAME")); value != "" {
		return value
	}
	if value := strings.TrimSpace(c.Telemetry.ServiceName); value != "" {
		return value
	}
	return "domour"
}

func (c DomourConfig) TelemetryServiceVersion() string {
	if value := strings.TrimSpace(os.Getenv("DOMOUR_TELEMETRY_SERVICE_VERSION")); value != "" {
		return value
	}
	if value := strings.TrimSpace(c.Telemetry.ServiceVersion); value != "" {
		return value
	}
	return "dev"
}

func (c DomourConfig) TelemetryEndpoint() string {
	if value := strings.TrimSpace(os.Getenv("DOMOUR_TELEMETRY_ENDPOINT")); value != "" {
		return value
	}
	return strings.TrimSpace(c.Telemetry.Endpoint)
}

func (c DomourConfig) TelemetrySampler() float64 {
	if value := strings.TrimSpace(os.Getenv("DOMOUR_TELEMETRY_SAMPLER")); value != "" {
		var f float64
		if _, err := fmt.Sscanf(value, "%f", &f); err == nil {
			return f
		}
	}
	return c.Telemetry.Sampler
}

func (c DomourConfig) TelemetryUseStdout() bool {
	if value := strings.ToLower(strings.TrimSpace(os.Getenv("DOMOUR_TELEMETRY_USE_STDOUT"))); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return c.Telemetry.UseStdout
}

func (c DomourConfig) IsLogAsJSON() bool {
	if value := strings.ToLower(strings.TrimSpace(os.Getenv("DOMOUR_LOG_AS_JSON"))); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return c.LogAsJSON
}

func (c DomourConfig) IsDebug() bool {
	if value := strings.ToLower(strings.TrimSpace(os.Getenv("DOMOUR_DEBUG"))); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return false
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
	home := DomourHomeDir()
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
		MCPDir:     filepath.Join(home, "mcp"),
		SkillsDir:  filepath.Join(home, "skills"),
		MCPServers: map[string]MCPServerConfig{},
	}
}

func normalizeDomourConfig(cfg DomourConfig) DomourConfig {
	cfg.HTTPSProxy = strings.TrimSpace(cfg.HTTPSProxy)
	if cfg.HTTPSProxy == "" {
		cfg.HTTPSProxy = DefaultHTTPSProxy
	}
	cfg.DefaultProvider = normalizeProviderKey(cfg.DefaultProvider)
	cfg.DefaultModel = strings.TrimSpace(cfg.DefaultModel)

	home := DomourHomeDir()
	homeDir, _ := os.UserHomeDir()
	cfg.MCPDir = strings.TrimSpace(cfg.MCPDir)
	if cfg.MCPDir == "" {
		cfg.MCPDir = filepath.Join(home, "mcp")
	} else if strings.HasPrefix(cfg.MCPDir, "~") && homeDir != "" {
		cfg.MCPDir = filepath.Join(homeDir, cfg.MCPDir[1:])
	}

	cfg.SkillsDir = strings.TrimSpace(cfg.SkillsDir)
	if cfg.SkillsDir == "" {
		cfg.SkillsDir = filepath.Join(home, "skills")
	} else if strings.HasPrefix(cfg.SkillsDir, "~") && homeDir != "" {
		cfg.SkillsDir = filepath.Join(homeDir, cfg.SkillsDir[1:])
	}

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

	normalizedModes := make(map[string]ModeMapping, len(cfg.ModeMappings))
	for key, mm := range cfg.ModeMappings {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		mm.Require = normalizeTags(mm.Require)
		mm.Prefer = normalizeTags(mm.Prefer)
		mm.Exclude = normalizeTags(mm.Exclude)
		normalizedModes[normalizedKey] = mm
	}
	cfg.ModeMappings = normalizedModes
	if cfg.ModeMappings == nil {
		cfg.ModeMappings = map[string]ModeMapping{}
	}

	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]MCPServerConfig{}
	} else {
		normalizedMCPServers := make(map[string]MCPServerConfig, len(cfg.MCPServers))
		for key, mcpCfg := range cfg.MCPServers {
			mcpCfg.Type = strings.ToLower(strings.TrimSpace(mcpCfg.Type))
			mcpCfg.Command = strings.TrimSpace(mcpCfg.Command)
			mcpCfg.URL = strings.TrimSpace(mcpCfg.URL)
			mcpCfg.AppID = strings.TrimSpace(mcpCfg.AppID)
			normalizedMCPServers[strings.TrimSpace(key)] = mcpCfg
		}
		cfg.MCPServers = normalizedMCPServers
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
	case "deepseek":
		return "deepseek"
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
	case "llamacpp", "llama.cpp", "llama_cpp":
		return "llamacpp"
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

func normalizeTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// IsProviderInUse returns true if the provider is currently in use or explicitly enabled.
func (c DomourConfig) IsProviderInUse(provider string) bool {
	p := normalizeProviderKey(provider)
	if p == "" {
		return false
	}

	// Check default provider
	if c.DefaultProviderName() == p {
		return true
	}

	// Check entries
	for _, entry := range c.Entries {
		if normalizeProviderKey(entry.Provider) == p {
			return true
		}
	}

	// Check if explicitly enabled in providers config
	if c.Providers != nil {
		if pc, ok := c.Providers[p]; ok && pc.Enabled {
			return true
		}
	}

	return false
}