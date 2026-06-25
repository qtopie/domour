// Package model provides a model registry with tag-based querying.
//
// Models are registered with a set of system tags (e.g. "local", "free", "flash",
// "pro", "deep", "lite") and optional user-defined tags. The registry supports
// querying by require+prefer+exclude tag rules.
package model

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registration describes a model registered in the registry.
type Registration struct {
	ID          string   // Unique model identifier, e.g. "gemma4-e4b"
	Provider    string   // Provider name, e.g. "llamacpp"
	ModelName   string   // Model name for the provider, e.g. "gemma4:e4b"
	Tags        []string // System-level tags (local, private, free, flash, pro, deep, lite, ...)
	UserTags    []string // User-defined tags (arbitrary)
}

// Registry is a thread-safe model registry that supports tag-based querying.
type Registry struct {
	mu   sync.RWMutex
	byID map[string]*Registration
}

// NewRegistry creates an empty model registry.
func NewRegistry() *Registry {
	return &Registry{
		byID: make(map[string]*Registration),
	}
}

// Register adds or updates a model in the registry.
func (r *Registry) Register(reg Registration) error {
	id := strings.TrimSpace(reg.ID)
	if id == "" {
		return fmt.Errorf("model registry: id is required")
	}
	if strings.TrimSpace(reg.Provider) == "" {
		return fmt.Errorf("model registry: provider is required for %q", id)
	}
	if strings.TrimSpace(reg.ModelName) == "" {
		return fmt.Errorf("model registry: model_name is required for %q", id)
	}

	// Normalize tags
	reg.Tags = normalizeTags(reg.Tags)
	reg.UserTags = normalizeTags(reg.UserTags)

	r.mu.Lock()
	r.byID[id] = &Registration{
		ID:        id,
		Provider:  reg.Provider,
		ModelName: reg.ModelName,
		Tags:      reg.Tags,
		UserTags:  reg.UserTags,
	}
	r.mu.Unlock()
	return nil
}

// Unregister removes a model from the registry.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	delete(r.byID, id)
	r.mu.Unlock()
}

// List returns all registered models.
func (r *Registry) List() []Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Registration, 0, len(r.byID))
	for _, reg := range r.byID {
		out = append(out, *reg)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// QueryBestModel selects the best matching model based on tag rules.
//
// The matching algorithm works as follows:
//  1. require: All tags in this list must be present. Models missing any required
//     tag are excluded.
//  2. exclude: Models having any tag in this list are excluded.
//  3. prefer: Remaining models are scored by how many prefer tags they match.
//     The model with the highest score wins. If multiple models tie, the first
//     one (sorted by ID) is returned.
//
// Returns ("", "", nil) if no model matches.
func (r *Registry) QueryBestModel(require, prefer, exclude []string) (provider, modelName string, err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	requireSet := toSet(require)
	preferSet := toSet(prefer)
	excludeSet := toSet(exclude)

	var best *Registration
	var bestScore int

	for _, reg := range r.byID {
		// Combine system tags and user tags for matching
		regTags := toSet(append(reg.Tags, reg.UserTags...))

		// Check exclude: if any exclude tag is present, skip
		if hasAny(regTags, excludeSet) {
			continue
		}

		// Check require: all required tags must be present
		if !containsAll(regTags, requireSet) {
			continue
		}

		// Score by prefer tags
		score := countMatches(regTags, preferSet)

		if best == nil || score > bestScore || (score == bestScore && reg.ID < best.ID) {
			best = reg
			bestScore = score
		}
	}

	if best == nil {
		return "", "", nil
	}
	return best.Provider, best.ModelName, nil
}

// --- helpers ---

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

func toSet(ss []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		out[strings.TrimSpace(strings.ToLower(s))] = struct{}{}
	}
	return out
}

func hasAny(tags, exclude map[string]struct{}) bool {
	for t := range tags {
		if _, ok := exclude[t]; ok {
			return true
		}
	}
	return false
}

func containsAll(tags, require map[string]struct{}) bool {
	for t := range require {
		if _, ok := tags[t]; ok {
			continue
		}
		return false
	}
	return true
}

func countMatches(tags, prefer map[string]struct{}) int {
	count := 0
	for t := range tags {
		if _, ok := prefer[t]; ok {
			count++
		}
	}
	return count
}
