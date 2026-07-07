package skill

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// InputsDefinition describes the input schema needed for the skill.
type InputsDefinition struct {
	Required []string `json:"required,omitempty" yaml:"required,omitempty"`
	Optional []string `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// ToolDefinition represents a tool inside the skill.
type ToolDefinition struct {
	Name        string          `json:"name" yaml:"name"`
	Description string          `json:"description" yaml:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty" yaml:"parameters,omitempty"` // Keep as raw JSON to pass to GenAI
}

// UnmarshalYAML implements yaml.Unmarshaler for ToolDefinition to decode parameters into JSON raw bytes.
func (t *ToolDefinition) UnmarshalYAML(value *yaml.Node) error {
	var temp struct {
		Name        string      `yaml:"name"`
		Description string      `yaml:"description"`
		Parameters  interface{} `yaml:"parameters,omitempty"`
	}
	if err := value.Decode(&temp); err != nil {
		return err
	}
	t.Name = temp.Name
	t.Description = temp.Description
	if temp.Parameters != nil {
		jsonBytes, err := json.Marshal(temp.Parameters)
		if err != nil {
			return fmt.Errorf("failed to marshal parameters to JSON: %w", err)
		}
		t.Parameters = jsonBytes
	}
	return nil
}

// Skill represents the unified skill specification.
type Skill struct {
	ID           string           `json:"id" yaml:"id"`
	Name         string           `json:"name" yaml:"name"`
	Description  string           `json:"description,omitempty" yaml:"description,omitempty"`
	IntentTags   []string         `json:"intent_tags,omitempty" yaml:"intent_tags,omitempty"`
	Instructions string           `json:"instructions,omitempty" yaml:"instructions,omitempty"` // Cognitive prompts / guidelines
	Inputs       InputsDefinition `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Tools        []ToolDefinition `json:"tools,omitempty" yaml:"tools,omitempty"` // Tools used by this skill
	Version      string           `json:"version,omitempty" yaml:"version,omitempty"`
	CreatedAt    int64            `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt    int64            `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}
