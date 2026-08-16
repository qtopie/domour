package engine

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/qtopie/domour/internal/cognitor/proxy"
	appconfig "github.com/qtopie/domour/internal/config"
)

// fallbackCandidate is a provider/model pair to try during auto-fallback.
type fallbackCandidate struct {
	provider string
	model    string
}

// buildClientFunc builds a *proxy.Client for an entry + provider + model.
type buildClientFunc func(ctx context.Context, entry, provider, model string) (*proxy.Client, error)

// readyFunc reports whether a client is ready for use.
type readyFunc func(ctx context.Context, c *proxy.Client) (bool, error)

// fallbackCandidates returns the priority-ordered candidate list for a request.
//
// Priority order (highest first):
//  1. The requested provider (with requested model, or its configured model).
//  2. The entry's configured provider, if different from #1.
//  3. The default provider, if different from #1/#2.
//  4. Remaining enabled providers that have an api_key or base_url, in
//     lexicographic provider-name order.
//
// Disabled providers and providers with neither api_key nor base_url are
// never considered. The primary provider is never duplicated.
func fallbackCandidates(entry, requestedProvider, requestedModel string, cfg appconfig.DomourConfig) []fallbackCandidate {
	seen := make(map[string]bool)
	var out []fallbackCandidate
	add := func(p, m string) {
		p = strings.TrimSpace(p)
		m = strings.TrimSpace(m)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, fallbackCandidate{provider: p, model: m})
	}

	// 1. Requested provider (with requested model, or its configured model).
	add(requestedProvider, requestedModel)
	if len(out) > 0 && out[0].model == "" {
		out[0].model = cfg.ProviderModel(out[0].provider)
	}

	// 2. Entry's configured provider.
	entry = strings.ToLower(strings.TrimSpace(entry))
	if ep := cfg.EntryProvider(entry); ep != "" && !seen[ep] {
		em := cfg.EntryModel(entry)
		if em == "" {
			em = cfg.ProviderModel(ep)
		}
		add(ep, em)
	}

	// 3. Default provider.
	if dp := cfg.DefaultProviderName(); dp != "" && !seen[dp] {
		dm := cfg.DefaultModelName()
		if dm == "" {
			dm = cfg.ProviderModel(dp)
		}
		add(dp, dm)
	}

	// 4. Remaining enabled + configured providers, lexicographic order.
	var rest []string
	for name, pc := range cfg.Providers {
		norm := strings.ToLower(strings.TrimSpace(name))
		if seen[norm] {
			continue
		}
		if !pc.Enabled {
			continue
		}
		if strings.TrimSpace(pc.APIKey) == "" && strings.TrimSpace(pc.BaseURL) == "" {
			continue
		}
		rest = append(rest, norm)
	}
	sort.Strings(rest)
	for _, name := range rest {
		pc := cfg.Providers[name]
		m := pc.Model
		if len(pc.Models) > 0 {
			m = pc.Models[0]
		}
		add(name, m)
	}

	return out
}

// resolveWithFallback tries the candidate list in priority order and returns
// the first client that builds successfully. Fallback candidates (anything
// after the primary) must also pass the live readiness probe, since the caller
// only ever falls back to a provider that is verifiably usable. The primary is
// returned as-is on a successful build — the caller performs its own IsReady
// check, so we avoid doubling the live discovery call on the happy path.
// If no candidate works, it returns the primary error (from the first
// candidate's build failure).
func resolveWithFallback(ctx context.Context, entry, requestedProvider, requestedModel string, cfg appconfig.DomourConfig, build buildClientFunc, ready readyFunc) (*proxy.Client, error) {
	candidates := fallbackCandidates(entry, requestedProvider, requestedModel, cfg)

	var primaryErr error
	for i, cand := range candidates {
		cl, err := build(ctx, entry, cand.provider, cand.model)
		if err != nil {
			if i == 0 {
				primaryErr = err
				log.Printf("[Cognitor] Primary provider %q unavailable, trying fallbacks: %v", cand.provider, err)
			} else {
				log.Printf("[Cognitor] Fallback provider %q unavailable: %v", cand.provider, err)
			}
			continue
		}
		if i > 0 {
			ok, rerr := ready(ctx, cl)
			if !ok || rerr != nil {
				verr := rerr
				if verr == nil {
					verr = fmt.Errorf("provider %s is not ready", cand.provider)
				}
				log.Printf("[Cognitor] Fallback provider %q not ready: %v", cand.provider, verr)
				continue
			}
			log.Printf("[Cognitor] Auto-fallback selected provider=%q model=%q (primary was %q)", cand.provider, cand.model, candidates[0].provider)
		}
		return cl, nil
	}

	if primaryErr == nil {
		primaryErr = fmt.Errorf("no usable provider found for entry %q", entry)
	}
	return nil, primaryErr
}
