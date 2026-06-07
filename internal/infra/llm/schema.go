package llm

import (
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant/shared"
)

func HistoryToSchema(history []shared.Message, memorySummary string) []*schema.Message {
	messages := make([]*schema.Message, 0, len(history)+1)
	if strings.TrimSpace(memorySummary) != "" {
		messages = append(messages, schema.SystemMessage("[Previous Conversation Background & Memory Summary]:\n"+strings.TrimSpace(memorySummary)))
	}
	for _, item := range history {
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

func StripCodeFence(content string) string {
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
