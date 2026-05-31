package skill

import (
	"encoding/json"
)

// Skill represents a loaded skill from a Markdown file
type Skill struct {
	Name         string
	Description  string
	Instructions string
	Tools        []ToolDefinition
}

// ToolDefinition represents a tool inside the skill
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // Keep as raw JSON to pass to GenAI
}
