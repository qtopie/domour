package assistant

import (
	"context"
	"strings"

	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	appconfig "github.com/qtopie/domour/internal/config"
	domourmodel "github.com/qtopie/domour/pkg/model"
)

// ListModels returns a combined list of models from:
//  1. The global extension registry (llamacpp, etc.)
//  2. Domour's configured providers (deepseek, openai, gemini, etc.)
//
// Each model includes its provider name and capability tags so the client
// can display and allow user selection.
func (s *AssistantService) ListModels(ctx context.Context) ([]*chatpb.ModelInfo, error) {
	var out []*chatpb.ModelInfo
	seen := make(map[string]bool)

	// 1. Models from the global extension registry (e.g. llamacpp)
	for _, m := range domourmodel.DefaultRegistry().List() {
		key := m.Provider + ":" + m.ModelName
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, &chatpb.ModelInfo{
			Id:        m.ID,
			Provider:  m.Provider,
			ModelName: m.ModelName,
			Tags:      append([]string{}, m.Tags...),
			UserTags:  append([]string{}, m.UserTags...),
		})
	}

	// 2. Models from Domour's config providers
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		// Non-fatal — return what we have
		return out, nil
	}

	for providerName, pc := range cfg.Providers {
		normalized := strings.ToLower(strings.TrimSpace(providerName))

		// Use provider-level model list if available
		models := pc.Models
		if len(models) == 0 && pc.Model != "" {
			models = []string{pc.Model}
		}

		// Infer tags from proxy registry metadata
		tags := inferProviderTags(normalized)

		for _, modelName := range models {
			id := normalized + "-" + modelName
			key := normalized + ":" + modelName
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, &chatpb.ModelInfo{
				Id:        id,
				Provider:  normalized,
				ModelName: modelName,
				Tags:      tags,
			})
		}
	}

	return out, nil
}

// RegisterConfigProviderModels registers models from the Domour configuration
// providers (deepseek, openai, gemini, etc.) into the global model registry.
// This ensures tag-based mode selection (e.g. balanced → flash) can find them.
func RegisterConfigProviderModels(cfg *appconfig.DomourConfig) {
	if cfg == nil {
		return
	}
	for providerName, pc := range cfg.Providers {
		normalized := strings.ToLower(strings.TrimSpace(providerName))

		models := pc.Models
		if len(models) == 0 && pc.Model != "" {
			models = []string{pc.Model}
		}

		tags := inferProviderTags(normalized)

		for _, modelName := range models {
			id := normalized + "-" + modelName
			_ = domourmodel.DefaultRegistry().Register(domourmodel.Registration{
				ID:        id,
				Provider:  normalized,
				ModelName: modelName,
				Tags:      tags,
			})
		}
	}
}

// inferProviderTags returns capability tags for a provider based on its type.
func inferProviderTags(provider string) []string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "llamacpp", "ollama":
		return []string{"local", "private", "free"}
	case "deepseek":
		return []string{"cloud", "flash", "pro"}
	case "openai":
		return []string{"cloud", "pro"}
	case "gemini", "gemini-cli", "gemini_cli":
		return []string{"cloud", "pro"}
	case "claude":
		return []string{"cloud", "pro"}
	case "github-copilot-cli", "github-copilot":
		return []string{"cloud", "flash"}
	case "qodercli", "qoder-cli", "qoder":
		return []string{"cloud", "flash"}
	default:
		return []string{"cloud"}
	}
}
