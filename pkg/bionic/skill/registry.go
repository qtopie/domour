package skill

import (
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Skill represents the public skill definition.
type Skill struct {
	Name         string   `json:"name" yaml:"name"`
	Description  string   `json:"description,omitempty" yaml:"description,omitempty"`
	Instructions string   `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	IntentTags   []string `json:"intent_tags,omitempty" yaml:"intent_tags,omitempty"`
}

var (
	mu     sync.Mutex
	skills []Skill
)

// Register registers a public skill.
func Register(s Skill) {
	mu.Lock()
	defer mu.Unlock()
	skills = append(skills, s)
}

// List returns all registered public skills.
func List() []Skill {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Skill, len(skills))
	copy(out, skills)
	return out
}

// ParseSkill parses a skill from markdown content with YAML frontmatter.
func ParseSkill(content string) (Skill, error) {
	trimmed := strings.TrimSpace(content)
	var s Skill
	if strings.HasPrefix(trimmed, "---") {
		parts := strings.SplitN(trimmed, "---", 3)
		if len(parts) >= 3 {
			yamlContent := parts[1]
			bodyContent := parts[2]
			if err := yaml.Unmarshal([]byte(yamlContent), &s); err != nil {
				return s, err
			}
			if s.Instructions == "" {
				s.Instructions = strings.TrimSpace(bodyContent)
			}
			return s, nil
		}
	}
	s.Instructions = trimmed
	return s, nil
}
