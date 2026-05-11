package modelmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appconfig "github.com/qtopie/domour/internal/app/config"
	"github.com/qtopie/domour/internal/pkg/brain/diencephalon"
	brainllm "github.com/qtopie/domour/pkg/core/llm"
)

type DiscoverRequest struct {
	Entry    string `json:"entry,omitempty"`
	Provider string `json:"provider,omitempty"`
}

type DiscoverResponse struct {
	ConfigPath         string   `json:"config_path,omitempty"`
	Entry              string   `json:"entry,omitempty"`
	Provider           string   `json:"provider"`
	SelectedModel      string   `json:"selected_model,omitempty"`
	Models             []string `json:"models,omitempty"`
	Source             string   `json:"source,omitempty"`
	DiscoverySupported bool     `json:"discovery_supported"`
	Cached             bool     `json:"cached,omitempty"`
	Message            string   `json:"message,omitempty"`
}

type SetModelRequest struct {
	Entry    string `json:"entry,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model"`
}

type SetModelResponse struct {
	ConfigPath string `json:"config_path,omitempty"`
	Entry      string `json:"entry,omitempty"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
}

func Discover(ctx context.Context, req DiscoverRequest) (DiscoverResponse, error) {
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return DiscoverResponse{}, err
	}
	configPath, err := appconfig.DomourConfigPath()
	if err != nil {
		return DiscoverResponse{}, err
	}

	entry := normalizeEntry(req.Entry)
	resolvedCfg := diencephalon.ResolveConfig(entry, cfg)
	provider := strings.TrimSpace(req.Provider)
	if provider != "" {
		resolvedCfg.Provider = provider
		resolvedCfg.APIKey = cfg.APIKeyForProvider(provider)
		resolvedCfg.BaseURL = cfg.BaseURLForProvider(provider)
		resolvedCfg.ProxyURL = cfg.ProxyForProvider(provider)
		resolvedCfg.Model = firstNonEmpty(cfg.ProviderModel(provider), resolvedCfg.Model)
	}

	resp := DiscoverResponse{
		ConfigPath:    configPath,
		Entry:         entry,
		Provider:      strings.TrimSpace(resolvedCfg.Provider),
		SelectedModel: strings.TrimSpace(resolvedCfg.Model),
	}

	result, err := brainllm.DiscoverModels(ctx, &brainllm.Config{
		Provider: resolvedCfg.Provider,
		APIKey:   resolvedCfg.APIKey,
		BaseURL:  resolvedCfg.BaseURL,
		Model:    resolvedCfg.Model,
		ProxyURL: resolvedCfg.ProxyURL,
	})
	if err == nil {
		resp.DiscoverySupported = true
		resp.Models = append([]string(nil), result.Models...)
		resp.Source = result.Source
		cfg.SetProviderDiscoveredModels(resp.Provider, result.Models)
		if saveErr := appconfig.SaveDomourConfig(cfg); saveErr != nil {
			return DiscoverResponse{}, saveErr
		}
		return resp, nil
	}
	if !errors.Is(err, brainllm.ErrModelDiscoveryUnsupported) {
		return DiscoverResponse{}, err
	}

	resp.Models = cfg.ProviderModels(resp.Provider)
	if len(resp.Models) == 0 && resp.SelectedModel != "" {
		resp.Models = []string{resp.SelectedModel}
	}
	resp.Cached = len(resp.Models) > 0
	resp.Message = fmt.Sprintf("provider %s does not expose model discovery; showing configured/cached models only", resp.Provider)
	return resp, nil
}

func SetModel(_ context.Context, req SetModelRequest) (SetModelResponse, error) {
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return SetModelResponse{}, err
	}
	configPath, err := appconfig.DomourConfigPath()
	if err != nil {
		return SetModelResponse{}, err
	}

	entry := normalizeEntry(req.Entry)
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return SetModelResponse{}, fmt.Errorf("model is required")
	}

	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		if entry == "" {
			provider = firstNonEmpty(cfg.DefaultProviderName(), "github-copilot-cli")
		} else {
			provider = firstNonEmpty(cfg.EntryProvider(entry), cfg.DefaultProviderName(), "github-copilot-cli")
		}
	}

	if entry == "" {
		cfg.SetDefaultSelection(provider, model)
	} else {
		cfg.SetEntrySelection(entry, provider, model)
	}

	if err := appconfig.SaveDomourConfig(cfg); err != nil {
		return SetModelResponse{}, err
	}
	return SetModelResponse{
		ConfigPath: configPath,
		Entry:      entry,
		Provider:   provider,
		Model:      model,
	}, nil
}

func normalizeEntry(entry string) string {
	entry = strings.ToLower(strings.TrimSpace(entry))
	if entry == "default" {
		return ""
	}
	return entry
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
