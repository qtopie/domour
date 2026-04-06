package mvp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type Output struct {
	Summary     string
	Route       string
	Format      string
	Title       string
	Diagram     string
	NeedsRender bool
}

type DiagramBrain struct{}

func NewDiagramBrain() *DiagramBrain {
	return &DiagramBrain{}
}

func (b *DiagramBrain) Think(_ context.Context, prompt string) (Output, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "画一个系统架构图"
	}

	format := inferFormat(prompt)
	title := inferTitle(prompt)
	diagram := buildD2Diagram(title, prompt, format)

	return Output{
		Summary:     fmt.Sprintf("Brain inferred a diagram request and chose %s rendering.", format),
		Route:       "render_d2",
		Format:      format,
		Title:       title,
		Diagram:     diagram,
		NeedsRender: true,
	}, nil
}

func inferFormat(prompt string) string {
	prompt = strings.ToLower(prompt)
	switch {
	case strings.Contains(prompt, "网页"), strings.Contains(prompt, "web"), strings.Contains(prompt, "html"), strings.Contains(prompt, "页面"):
		return "html"
	default:
		return "svg"
	}
}

func inferTitle(prompt string) string {
	cleaned := strings.TrimSpace(prompt)
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")
	if len(cleaned) > 48 {
		cleaned = cleaned[:48]
	}
	if cleaned == "" {
		return "System Architecture"
	}
	return cleaned
}

func buildD2Diagram(title, prompt, format string) string {
	return fmt.Sprintf(`direction: right

title: %q

user: User
agent: Agent
brain: Brain
motor: Motor
tool: "D2 Render Tool"
artifact: "%s output"

user -> agent: "chat request"
agent -> brain: "reason about request"
brain -> motor: "D2 diagram plan"
motor -> tool: "render diagram"
tool -> agent: "artifact"
agent -> user: "final response"

brain.note: "Prompt: %s"
`, title, strings.ToUpper(format), escapeLabel(prompt))
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `"`, `'`)
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
