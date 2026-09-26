package capability

import "context"

// SkillDefinition defines task-level orchestration and prompting contracts.
type SkillDefinition struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	IntentTags   []string `json:"intent_tags,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
	Tools        []string `json:"tools,omitempty"`
}

// SkillRegistry handles registration, discovery, and storage of skills.
type SkillRegistry interface {
	Register(ctx context.Context, s *SkillDefinition) error
	Get(ctx context.Context, id string) (*SkillDefinition, error)
	List(ctx context.Context) ([]*SkillDefinition, error)
	Delete(ctx context.Context, id string) error
}
