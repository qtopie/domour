package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/qtopie/domour/internal/infra/llm"
)

type DiscoveryProvider interface {
	Discover(ctx context.Context) ([]Entry, error)
	CheckHealth(ctx context.Context) (ResourceStatus, error)
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
			Status:   StatusOnline,
			LastCheck: time.Now().Unix(),
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

func (p *LLMDiscoveryProvider) CheckHealth(ctx context.Context) (ResourceStatus, error) {
	_, err := llm.DiscoverModels(ctx, &p.Config)
	if err != nil {
		return StatusOffline, err
	}
	return StatusOnline, nil
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

func StartPeriodicHealthCheck(ctx context.Context, r *Registry, providers []DiscoveryProvider, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, p := range providers {
					lp, ok := p.(*LLMDiscoveryProvider)
					if !ok {
						continue
					}

					// Optimization: Check if any entry for this provider was recently updated
					// (e.g. by a successful message reply)
					entries := r.List(func(e Entry) bool {
						return e.Provider == lp.Config.Provider
					})

					shouldCheck := true
					now := time.Now().Unix()
					for _, e := range entries {
						// If updated within 80% of the interval, skip active check
						if now-e.LastCheck < int64(interval.Seconds()*0.8) {
							shouldCheck = false
							break
						}
					}

					if !shouldCheck {
						continue
					}

					status, _ := p.CheckHealth(ctx)
					for _, e := range entries {
						r.SetStatus(e.ID, status)
					}
				}
			}
		}
	}()
}
