// Package hub provides the resource management and capability registration
// center for the Domour agent runtime.
package hub

import (
	"context"
	"fmt"
	"sort"

	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/config"
)

// ToolManifest represents a tool's details including its lifecycle status.
type ToolManifest struct {
	Name        string            `json:"name"`
	Kind        string            `json:"kind"`
	Description string            `json:"description,omitempty"`
	Loaded      bool              `json:"loaded"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// ToolDefinition represents a simplified view of a tool within a skill.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillManifest represents a high-level agent skill definition.
type SkillManifest struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	SourcePath   string            `json:"source_path,omitempty"`
	Provider     string            `json:"provider,omitempty"`
	Format       string            `json:"format,omitempty"`
	Loaded       bool              `json:"loaded"`
	Tools        []ToolDefinition  `json:"tools,omitempty"`
}

// ProviderInfo represents an LLM provider's configuration and available models.
type ProviderInfo struct {
	ID                    string   `json:"id"`
	BaseURL               string   `json:"base_url,omitempty"`
	APIKey                string   `json:"api_key,omitempty"`
	Model                 string   `json:"model,omitempty"`
	Models                []string `json:"models,omitempty"`
	HTTPSProxy            string   `json:"https_proxy,omitempty"`
	Enabled               bool     `json:"enabled"`
	MaxActiveTokens       int      `json:"max_active_tokens,omitempty"`
	CompressTriggerTokens int      `json:"compress_trigger_tokens,omitempty"`
	Healthy               bool     `json:"healthy"`
	LastError             string   `json:"last_error,omitempty"`
}

// ToolReader defines the read-only operations for discovering available tools.
type ToolReader interface {
	// ListTools returns a list of all registered tools in the system.
	ListTools(ctx context.Context) ([]*ToolManifest, error)
	// GetTool retrieves metadata for a specific tool by name.
	GetTool(ctx context.Context, id string) (*ToolManifest, error)
}

// SkillReader defines the read-only operations for discovering agent skills.
type SkillReader interface {
	// ListSkills returns a list of all registered skills.
	ListSkills(ctx context.Context) ([]*SkillManifest, error)
	// GetSkill retrieves metadata for a specific skill by name.
	GetSkill(ctx context.Context, id string) (*SkillManifest, error)
}

// ProviderManager defines the operations for managing LLM providers.
type ProviderManager interface {
	// ListProviders returns all configured LLM providers.
	ListProviders(ctx context.Context) ([]*ProviderInfo, error)
	// GetProvider retrieves configuration for a specific provider by ID.
	GetProvider(ctx context.Context, id string) (*ProviderInfo, error)
	// SaveProvider persists provider configuration.
	SaveProvider(ctx context.Context, p *ProviderInfo) error
	// ToggleProviderStatus enables or disables a provider.
	ToggleProviderStatus(ctx context.Context, id string, enable bool) error
}

// ArkHub aggregates ToolReader, SkillReader, and ProviderManager into a single
// capability management interface.
type ArkHub interface {
	ToolReader
	SkillReader
	ProviderManager
}

// ToolResolver provides methods for retrieving tool and skill manifests.
type ToolResolver interface {
	ListTools(ctx context.Context) ([]*ToolManifest, error)
	GetTool(ctx context.Context, id string) (*ToolManifest, error)
	ListSkills(ctx context.Context) ([]*SkillManifest, error)
	GetSkill(ctx context.Context, id string) (*SkillManifest, error)
}

type arkHub struct {
	resolver ToolResolver
}

// Option configures an ArkHub instance.
type Option func(*arkHub)

// WithToolResolver attaches a custom ToolResolver to the Hub.
func WithToolResolver(tr ToolResolver) Option {
	return func(h *arkHub) {
		h.resolver = tr
	}
}

// NewArkHub constructs a new ArkHub instance with optional functional configurations.
func NewArkHub(opts ...Option) ArkHub {
	h := &arkHub{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// NewArkHubWithResolver constructs an ArkHub with a specified ToolResolver.
func NewArkHubWithResolver(tr ToolResolver) ArkHub {
	return &arkHub{resolver: tr}
}

// ListTools lists all registered tools.
func (h *arkHub) ListTools(ctx context.Context) ([]*ToolManifest, error) {
	if h.resolver == nil {
		return []*ToolManifest{}, nil
	}
	return h.resolver.ListTools(ctx)
}

// GetTool retrieves a tool by ID.
func (h *arkHub) GetTool(ctx context.Context, id string) (*ToolManifest, error) {
	if h.resolver == nil {
		return nil, fmt.Errorf("tool %q not found", id)
	}
	return h.resolver.GetTool(ctx, id)
}

// ListSkills lists all registered skills.
func (h *arkHub) ListSkills(ctx context.Context) ([]*SkillManifest, error) {
	if h.resolver == nil {
		return []*SkillManifest{}, nil
	}
	return h.resolver.ListSkills(ctx)
}

// GetSkill retrieves a skill by ID.
func (h *arkHub) GetSkill(ctx context.Context, id string) (*SkillManifest, error) {
	if h.resolver == nil {
		return nil, fmt.Errorf("skill %q not found", id)
	}
	return h.resolver.GetSkill(ctx, id)
}


// ListProviders lists all configured providers.
func (h *arkHub) ListProviders(ctx context.Context) ([]*ProviderInfo, error) {
	cfg, err := config.LoadDomourConfig()
	if err != nil {
		return nil, err
	}
	var list []*ProviderInfo
	for id, p := range cfg.Providers {
		healthy, checkErr := proxy.GetProviderHealthStatus(id)
		var lastErr string
		if checkErr != nil {
			lastErr = checkErr.Error()
		}
		list = append(list, &ProviderInfo{
			ID:                    id,
			BaseURL:               p.BaseURL,
			APIKey:                p.APIKey,
			Model:                 p.Model,
			Models:                p.Models,
			HTTPSProxy:            p.HTTPSProxy,
			Enabled:               p.Enabled,
			MaxActiveTokens:       p.MaxActiveTokens,
			CompressTriggerTokens: p.CompressTriggerTokens,
			Healthy:               healthy,
			LastError:             lastErr,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})
	return list, nil
}

// GetProvider retrieves a provider configuration by ID.
func (h *arkHub) GetProvider(ctx context.Context, id string) (*ProviderInfo, error) {
	cfg, err := config.LoadDomourConfig()
	if err != nil {
		return nil, err
	}
	p, ok := cfg.Providers[id]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", id)
	}
	healthy, checkErr := proxy.GetProviderHealthStatus(id)
	var lastErr string
	if checkErr != nil {
		lastErr = checkErr.Error()
	}
	return &ProviderInfo{
		ID:                    id,
		BaseURL:               p.BaseURL,
		APIKey:                p.APIKey,
		Model:                 p.Model,
		Models:                p.Models,
		HTTPSProxy:            p.HTTPSProxy,
		Enabled:               p.Enabled,
		MaxActiveTokens:       p.MaxActiveTokens,
		CompressTriggerTokens: p.CompressTriggerTokens,
		Healthy:               healthy,
		LastError:             lastErr,
	}, nil
}

// SaveProvider creates or updates a provider configuration.
func (h *arkHub) SaveProvider(ctx context.Context, p *ProviderInfo) error {
	if p == nil || p.ID == "" {
		return fmt.Errorf("invalid provider info")
	}
	cfg, err := config.LoadDomourConfig()
	if err != nil {
		return err
	}
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]config.ProviderConfig)
	}
	cfg.Providers[p.ID] = config.ProviderConfig{
		BaseURL:               p.BaseURL,
		APIKey:                p.APIKey,
		Model:                 p.Model,
		Models:                p.Models,
		HTTPSProxy:            p.HTTPSProxy,
		Enabled:               p.Enabled,
		MaxActiveTokens:       p.MaxActiveTokens,
		CompressTriggerTokens: p.CompressTriggerTokens,
	}
	return config.SaveDomourConfig(cfg)
}

// ToggleProviderStatus enables or disables a provider.
func (h *arkHub) ToggleProviderStatus(ctx context.Context, id string, enable bool) error {
	cfg, err := config.LoadDomourConfig()
	if err != nil {
		return err
	}
	p, ok := cfg.Providers[id]
	if !ok {
		return fmt.Errorf("provider %q not found", id)
	}
	p.Enabled = enable
	cfg.Providers[id] = p
	return config.SaveDomourConfig(cfg)
}
