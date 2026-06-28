# Unified Skills Engine Design

This document outlines the design for providing unified, storage-independent Skill support in Domour, bridging local and distributed Dapr agent networks.

---

## 1. Background & Motivation
Skills represent **task-level orchestration contracts** that outline when to use a skill, what tools or inputs are required, and the cognitive prompts or guidelines.

Currently, skills are discovered exclusively from the local filesystem (such as `.md` files). To support distributed runtimes and dynamic capabilities:
1. Skills must be decoupled from the local filesystem.
2. We need a unified **Skill Registry** interface.
3. The Cerebrum (Cognitive layer) must be able to synthesize new skills dynamically and save them.
4. Runtimes must support distributed stores (e.g., Dapr State Store or databases like SurrealDB) alongside local storage, allowing seamless service discovery and isolation.

---

## 2. Domain Model: Unified Skill

We redefine the `Skill` model in `internal/bionic/skill/skill.go`:

```go
package skill

// InputsDefinition describes the input schema needed for the skill.
type InputsDefinition struct {
	Required []string `json:"required,omitempty"`
	Optional []string `json:"optional,omitempty"`
}

// Skill represents the unified skill specification.
type Skill struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  string           `json:"description,omitempty"`
	IntentTags   []string         `json:"intent_tags,omitempty"`
	Instructions string           `json:"instructions,omitempty"` // Prompts for the Brain/Cerebrum
	Inputs       InputsDefinition `json:"inputs,omitempty"`
	Tools        []string         `json:"tools,omitempty"`        // Atomic tools used by this skill
	Version      string           `json:"version,omitempty"`
	CreatedAt    int64            `json:"created_at,omitempty"`
	UpdatedAt    int64            `json:"updated_at,omitempty"`
}
```

---

## 3. Unified Skill Registry Abstraction

We define the interface `Registry` in `internal/bionic/skill/registry.go`:

```go
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
```

---

## 4. Storage Providers (Registries)

We implement three types of Registries to support diverse deployment modes:

### A. Memory Registry (`MemoryRegistry`)
- Useful for transient skills, testing, or isolated agent runtimes.
- Uses a thread-safe map to store skills.

### B. File Registry (`FileRegistry`)
- Primarily for local configuration files (loads `.md` files or JSON skill manifests).
- Acts as a read-only or read-write local fallback.

### C. Dapr Registry (`DaprRegistry`)
- Uses the **Dapr State Store** component to save and retrieve skills.
- Allows multiple Dapr Agents to share a common registry in a distributed network.

---

## 5. Composite Discovery and Isolation

To bridge local fallback and remote capabilities, we introduce the `CompositeRegistry`:
- It wraps a list of registries (e.g. read-only local `FileRegistry` and read-write remote `DaprRegistry`).
- Resolves skills dynamically by checking backends in priority order.

```go
type CompositeRegistry struct {
	registries []Registry
}
```

### Dynamic Evolution:
- **Cerebrum (Brain)**: After complex reasoning, the Brain can generate a new skill specification and invoke `Registry.Register` to save the skill into the shared Dapr state store, propagating it instantly across the network.
- **Cerebellum**: Repeated choreographies or execution paths can be registered as localized skills in a `MemoryRegistry` or shared distributed registry.
- **Isolation**: Runtimes can choose which registry configurations to use, isolating local Edge nodes (e.g. local-only memory/file registry) from Cloud-based distributed agent pools.
