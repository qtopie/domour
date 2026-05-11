package registry

import (
	"context"
	"fmt"
	"strings"

	"github.com/qtopie/domour/pkg/core/llm"
)

type DiscoveryProvider interface {
	Discover(ctx context.Context) ([]Entry, error)
}

type LLMDiscoveryProvider struct {
	Config llm.Config
}

func (p *LLMDiscoveryProvider) Discover(ctx context.Context) ([]Entry, error) {
	result, err := llm.DiscoverModels(ctx, &p.Config)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, modelName := range result.Models {
		entry := Entry{
			ID:       fmt.Sprintf("%s:%s", result.Provider, modelName),
			Type:     ResourceLLM,
			Provider: result.Provider,
			Name:     modelName,
			Source:   result.Source,
		}

		// Basic capability inferring based on name for now
		lowerName := strings.ToLower(modelName)
		if strings.Contains(lowerName, "vision") || strings.Contains(lowerName, "vl") {
			entry.Capabilities = append(entry.Capabilities, CapVision, CapOCR)
		}

		entries = append(entries, entry)
	}
	return entries, nil
}

func DiscoverAndPopulate(ctx context.Context, r *Registry, providers []DiscoveryProvider) error {
	for _, p := range providers {
		entries, err := p.Discover(ctx)
		if err != nil {
			fmt.Printf("[Registry] Discovery error from provider: %v\n", err)
			continue
		}
		for _, entry := range entries {
			r.Register(entry)
		}
	}
	return nil
}
