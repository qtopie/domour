package skill

import (
	"encoding/json"
)

// InputsDefinition describes the input schema needed for the skill.
type InputsDefinition struct {
	Required []string `json:"required,omitempty"`
	Optional []string `json:"optional,omitempty"`
}

// ToolDefinition represents a tool inside the skill.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // Keep as raw JSON to pass to GenAI
}

// Skill represents the unified skill specification.
type Skill struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  string           `json:"description,omitempty"`
	IntentTags   []string         `json:"intent_tags,omitempty"`
	Instructions string           `json:"instructions,omitempty"` // Cognitive prompts / guidelines
	Inputs       InputsDefinition `json:"inputs,omitempty"`
	Tools        []ToolDefinition `json:"tools,omitempty"` // Tools used by this skill
	Version      string           `json:"version,omitempty"`
	CreatedAt    int64            `json:"created_at,omitempty"`
	UpdatedAt    int64            `json:"updated_at,omitempty"`
}
