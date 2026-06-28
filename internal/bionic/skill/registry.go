package skill

import "context"

// Registry is the unified interface to register, discover, and manage skills.
type Registry interface {
	// Register saves or updates a skill in the storage backend.
	Register(ctx context.Context, s *Skill) error

	// Get retrieves a specific skill specification by ID.
	Get(ctx context.Context, id string) (*Skill, error)

	// List returns all registered skills visible to this registry.
	List(ctx context.Context) ([]*Skill, error)

	// Delete removes a skill by its ID.
	Delete(ctx context.Context, id string) error
}
