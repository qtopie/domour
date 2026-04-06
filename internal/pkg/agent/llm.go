package agent

import (
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/pkg/copilot/shared"
)

func historyToSchema(history []shared.Message) []*schema.Message {
	if len(history) == 0 {
		return nil
	}
	start := 0
	if len(history) > 8 {
		start = len(history) - 8
	}

	messages := make([]*schema.Message, 0, len(history[start:]))
	for _, item := range history[start:] {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "assistant":
			messages = append(messages, schema.AssistantMessage(content, nil))
		case "system":
			messages = append(messages, schema.SystemMessage(content))
		default:
			messages = append(messages, schema.UserMessage(content))
		}
	}
	return messages
}

func stripCodeFence(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}

	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}
	lines = lines[1:]
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
