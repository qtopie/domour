package skill

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSkill parses a SKILL.md file, supporting both frontmatter-based standard skills
// and legacy heading-based format.
func ParseSkill(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	trimmed := strings.TrimSpace(content)

	skill := &Skill{}

	if strings.HasPrefix(trimmed, "---") {
		// Standard skill with YAML frontmatter
		parts := strings.SplitN(trimmed, "---", 3)
		if len(parts) >= 3 {
			yamlContent := parts[1]
			bodyContent := parts[2]

			// Decode the frontmatter
			if err := yaml.Unmarshal([]byte(yamlContent), skill); err != nil {
				return nil, fmt.Errorf("failed to parse YAML frontmatter: %w", err)
			}

			bodyTrimmed := strings.TrimSpace(bodyContent)
			if hasMarkdownSections(bodyTrimmed) {
				if err := parseMarkdownSections(bodyTrimmed, skill); err != nil {
					return nil, err
				}
			} else {
				if skill.Instructions == "" {
					skill.Instructions = bodyTrimmed
				}
			}
			return skill, nil
		}
	}

	// Legacy heading-only format
	if err := parseMarkdownSections(content, skill); err != nil {
		return nil, err
	}
	return skill, nil
}

func hasMarkdownSections(content string) bool {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "## Description") ||
			strings.HasPrefix(line, "## Instructions") ||
			strings.HasPrefix(line, "## Tools") {
			return true
		}
	}
	return false
}

func parseMarkdownSections(content string, skill *Skill) error {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var currentSection string
	var contentBuilder strings.Builder
	var toolJsonBuilder strings.Builder
	inCodeBlock := false

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "# ") {
			if skill.Name == "" {
				skill.Name = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			}
			continue
		}

		if strings.HasPrefix(line, "## ") {
			// Save previous section content
			saveSection(currentSection, contentBuilder.String(), skill)
			contentBuilder.Reset()

			currentSection = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}

		if currentSection == "Tools" {
			if strings.HasPrefix(line, "```json") {
				inCodeBlock = true
				continue
			}
			if strings.HasPrefix(line, "```") && inCodeBlock {
				inCodeBlock = false
				continue
			}
			if inCodeBlock {
				toolJsonBuilder.WriteString(line)
			}
		} else {
			contentBuilder.WriteString(line + "\n")
		}
	}

	// Save last section
	saveSection(currentSection, contentBuilder.String(), skill)

	// Parse Tools JSON only if Tools is not already populated via frontmatter
	if toolJsonBuilder.Len() > 0 && len(skill.Tools) == 0 {
		var tools []ToolDefinition
		if err := json.Unmarshal([]byte(toolJsonBuilder.String()), &tools); err != nil {
			return fmt.Errorf("failed to parse tools JSON: %w", err)
		}
		skill.Tools = tools
	}

	return nil
}

func saveSection(section, content string, skill *Skill) {
	content = strings.TrimSpace(content)
	switch section {
	case "Description":
		if skill.Description == "" {
			skill.Description = content
		}
	case "Instructions":
		if skill.Instructions == "" {
			skill.Instructions = content
		}
	}
}
