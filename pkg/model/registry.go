// Package model provides a public API for the Domour model registry.
//
// Extensions (e.g. cosmos-star ext.llamacpp) use this package to register
// available models with their capability tags. The brain then uses these tags
// to select the best model for the current system mode.
package model

import (
	"github.com/qtopie/domour/internal/infra/model"
)

// Registration describes a model's identity and capability tags.
type Registration struct {
	ID        string   // Unique identifier, e.g. "gemma4-e4b"
	Provider  string   // Provider name, e.g. "llamacpp"
	ModelName string   // Model name for the provider, e.g. "gemma4:e4b"
	Tags      []string // System capability tags: local, private, free, flash, pro, deep, lite, ...
	UserTags  []string // User-defined tags, e.g. ["gemma-family"]
}

// DefaultRegistry returns the singleton model registry used by the brain.
func DefaultRegistry() *internalRegistry { return internalRegistrySingleton }

// internalRegistry wraps the internal registry to expose only the public API.
type internalRegistry struct {
	inner *model.Registry
}

var internalRegistrySingleton = &internalRegistry{inner: model.NewRegistry()}

// Register adds or updates a model in the registry.
func (r *internalRegistry) Register(reg Registration) error {
	return r.inner.Register(model.Registration{
		ID:        reg.ID,
		Provider:  reg.Provider,
		ModelName: reg.ModelName,
		Tags:      reg.Tags,
		UserTags:  reg.UserTags,
	})
}

// Unregister removes a model from the registry.
func (r *internalRegistry) Unregister(id string) {
	r.inner.Unregister(id)
}

// List returns all registered models.
func (r *internalRegistry) List() []Registration {
	inner := r.inner.List()
	out := make([]Registration, len(inner))
	for i, reg := range inner {
		out[i] = Registration{
			ID:        reg.ID,
			Provider:  reg.Provider,
			ModelName: reg.ModelName,
			Tags:      reg.Tags,
			UserTags:  reg.UserTags,
		}
	}
	return out
}

// QueryBestModel selects the best matching model based on tag rules.
// Returns ("", "", nil) if no model matches.
func (r *internalRegistry) QueryBestModel(require, prefer, exclude []string) (provider, modelName string, err error) {
	return r.inner.QueryBestModel(require, prefer, exclude)
}
