package skill

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ParseSkill parses a SKILL.md file
func ParseSkill(path string) (*Skill, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	skill := &Skill{}
	scanner := bufio.NewScanner(file)

	var currentSection string
	var contentBuilder strings.Builder
	var toolJsonBuilder strings.Builder
	inCodeBlock := false

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "# ") {
			skill.Name = strings.TrimSpace(strings.TrimPrefix(line, "# "))
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

	// Parse Tools JSON
	if toolJsonBuilder.Len() > 0 {
		var tools []ToolDefinition
		if err := json.Unmarshal([]byte(toolJsonBuilder.String()), &tools); err != nil {
			return nil, fmt.Errorf("failed to parse tools JSON: %w", err)
		}
		skill.Tools = tools
	}

	return skill, nil
}

func saveSection(section, content string, skill *Skill) {
	content = strings.TrimSpace(content)
	switch section {
	case "Description":
		skill.Description = content
	case "Instructions":
		skill.Instructions = content
	}
}
