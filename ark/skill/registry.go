package skill

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// ReplaceByName registers a skill, replacing any existing entry with the same
// name. If no entry with that name exists, it appends.
func ReplaceByName(s Skill) {
	mu.Lock()
	defer mu.Unlock()
	for i, existing := range skills {
		if existing.Name == s.Name {
			skills[i] = s
			return
		}
	}
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

// LoadFromDir scans a directory for .md skill files (non-recursive), parses each
// with ParseSkill, and registers them via Register. Returns the list of
// successfully loaded skills.
func LoadFromDir(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir %s: %w", dir, err)
	}

	var loaded []Skill
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return loaded, fmt.Errorf("read skill file %s: %w", path, err)
		}
		s, err := ParseSkill(string(data))
		if err != nil {
			return loaded, fmt.Errorf("parse skill %s: %w", path, err)
		}
		if s.Name == "" {
			s.Name = strings.TrimSuffix(entry.Name(), ".md")
		}
		Register(s)
		loaded = append(loaded, s)
	}
	return loaded, nil
}

// LoadFromDirRecursive scans a directory recursively for .md skill files,
// parses each with ParseSkill, and registers them via Register. Returns the
// list of successfully loaded skills.
func LoadFromDirRecursive(dir string) ([]Skill, error) {
	var loaded []Skill
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read skill file %s: %w", path, err)
		}
		s, err := ParseSkill(string(data))
		if err != nil {
			return fmt.Errorf("parse skill %s: %w", path, err)
		}
		if s.Name == "" {
			s.Name = strings.TrimSuffix(d.Name(), ".md")
		}
		Register(s)
		loaded = append(loaded, s)
		return nil
	})
	if err != nil {
		return loaded, fmt.Errorf("walk skills dir %s: %w", dir, err)
	}
	return loaded, nil
}

// LoadFromDirs loads skills from multiple directories, merging and
// deduplicating by skill name. Directories are processed in order — when the
// same skill name appears in multiple directories, the last occurrence wins
// (higher priority). It returns the merged list and any errors encountered.
func LoadFromDirs(dirs ...string) ([]Skill, error) {
	seen := make(map[string]int) // skill name -> index in merged
	var merged []Skill
	var errs []error

	for _, dir := range dirs {
		loaded, err := LoadFromDir(dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", dir, err))
			continue
		}
		for _, s := range loaded {
			if idx, ok := seen[s.Name]; ok {
				merged[idx] = s // override: later wins
			} else {
				seen[s.Name] = len(merged)
				merged = append(merged, s)
			}
		}
	}

	if len(errs) > 0 {
		return merged, fmt.Errorf("errors loading skills: %v", errs)
	}
	return merged, nil
}
