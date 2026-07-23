package cognitor

import (
	"github.com/qtopie/domour/internal/infra/model"
)

// ModelRegistration describes a model's identity and capability tags.
type ModelRegistration struct {
	ID        string   // Unique identifier, e.g. "gemma4-e4b"
	Provider  string   // Provider name, e.g. "llamacpp"
	ModelName string   // Model name for the provider, e.g. "gemma4:e4b"
	Tags      []string // System capability tags: local, private, free, flash, pro, deep, lite, ...
	UserTags  []string // User-defined tags, e.g. ["gemma-family"]
}

// Registration is an alias to ModelRegistration for backward compatibility.
type Registration = ModelRegistration

// ModelRegistry wraps the model registry to expose only the public API.
type ModelRegistry struct {
	inner *model.Registry
}

var defaultModelRegistrySingleton = &ModelRegistry{inner: model.NewRegistry()}

// DefaultModelRegistry returns the singleton model registry used by the reasoning engine.
func DefaultModelRegistry() *ModelRegistry { return defaultModelRegistrySingleton }

// DefaultRegistry is an alias for DefaultModelRegistry.
func DefaultRegistry() *ModelRegistry { return defaultModelRegistrySingleton }

// Register adds or updates a model in the registry.
func (r *ModelRegistry) Register(reg ModelRegistration) error {
	return r.inner.Register(model.Registration{
		ID:        reg.ID,
		Provider:  reg.Provider,
		ModelName: reg.ModelName,
		Tags:      reg.Tags,
		UserTags:  reg.UserTags,
	})
}

// Unregister removes a model from the registry.
func (r *ModelRegistry) Unregister(id string) {
	r.inner.Unregister(id)
}

// List returns all registered models.
func (r *ModelRegistry) List() []ModelRegistration {
	inner := r.inner.List()
	out := make([]ModelRegistration, len(inner))
	for i, reg := range inner {
		out[i] = ModelRegistration{
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
func (r *ModelRegistry) QueryBestModel(require, prefer, exclude []string) (provider, modelName string, err error) {
	return r.inner.QueryBestModel(require, prefer, exclude)
}
