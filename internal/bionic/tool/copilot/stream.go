package copilot

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// EventType identifies the type of a JSONL event from Copilot CLI --output-format json.
type EventType string

const (
	EventTypeAssistant  EventType = "assistant"
	EventTypeTool       EventType = "tool"
	EventTypeToolResult EventType = "tool_result"
	EventTypeStats      EventType = "stats"
	EventTypeSystem     EventType = "system"
	EventTypeError      EventType = "error"
	EventTypeInfo       EventType = "info"
)

// Event represents one line of JSONL output from Copilot CLI.
type Event struct {
	Type EventType `json:"type"`

	// assistant event
	Content string `json:"content,omitempty"`

	// tool event (before execution)
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result event (after execution)
	Tool     string `json:"tool,omitempty"`
	Output   string `json:"output,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Error    string `json:"error,omitempty"`

	// stats event
	Tokens *TokenStats `json:"tokens,omitempty"`

	// system / info / error events
	Message string `json:"message,omitempty"`
	Level   string `json:"level,omitempty"`

	// Raw preserves the original JSON line for debugging or passthrough.
	Raw json.RawMessage `json:"-"`
}

// IsTerminal returns true for event types that signal the end of a session.
func (e *Event) IsTerminal() bool {
	return e.Type == EventTypeError ||
		(e.Type == EventTypeSystem && strings.EqualFold(e.Message, "done"))
}

// TokenStats contains the prompt/completion token usage reported by Copilot CLI.
type TokenStats struct {
	Prompt     int `json:"prompt"`
	Completion int `json:"completion"`
	Total      int `json:"total"`
}

// StreamParser reads Copilot CLI's JSONL output line by line and decodes it into Events.
type StreamParser struct {
	scanner *bufio.Scanner
}

// NewStreamParser wraps any io.Reader (typically cmd.Stdout) with a StreamParser.
func NewStreamParser(r io.Reader) *StreamParser {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1 MB per line
	return &StreamParser{scanner: scanner}
}

// Next reads and decodes the next event from the stream.
// Returns io.EOF when the stream is exhausted.
func (p *StreamParser) Next() (*Event, error) {
	for p.scanner.Scan() {
		line := strings.TrimSpace(p.scanner.Text())
		if line == "" {
			continue
		}
		ev, err := parseLine([]byte(line))
		if err != nil {
			// Non-JSON lines (e.g., banner text) are emitted as info events rather than errors.
			return &Event{
				Type:    EventTypeInfo,
				Message: line,
				Raw:     json.RawMessage(fmt.Sprintf("%q", line)),
			}, nil
		}
		ev.Raw = json.RawMessage(line)
		return ev, nil
	}
	if err := p.scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream scanner error: %w", err)
	}
	return nil, io.EOF
}

// Collect reads all events until EOF and returns them together with accumulated
// assistant text and tool results in a single Summary.
func (p *StreamParser) Collect() (*StreamSummary, error) {
	summary := &StreamSummary{}
	for {
		ev, err := p.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return summary, err
		}
		summary.Events = append(summary.Events, ev)
		switch ev.Type {
		case EventTypeAssistant:
			summary.AssistantText += ev.Content
		case EventTypeToolResult:
			if ev.Error != "" {
				summary.ToolErrors = append(summary.ToolErrors, fmt.Sprintf("[%s] %s", ev.Tool, ev.Error))
			} else {
				summary.ToolOutputs = append(summary.ToolOutputs, fmt.Sprintf("[%s]\n%s", ev.Tool, ev.Output))
			}
		case EventTypeStats:
			if ev.Tokens != nil {
				summary.Tokens = ev.Tokens
			}
		case EventTypeError:
			summary.FatalError = ev.Message
		}
	}
	return summary, nil
}

// StreamSummary aggregates the output of a full Copilot session into a digestible result.
type StreamSummary struct {
	Events        []*Event
	AssistantText string
	ToolOutputs   []string
	ToolErrors    []string
	FatalError    string
	Tokens        *TokenStats
}

// Observation builds a human-readable observation string for the Motor layer.
func (s *StreamSummary) Observation() string {
	parts := make([]string, 0, 4)
	if s.AssistantText != "" {
		parts = append(parts, s.AssistantText)
	}
	if len(s.ToolOutputs) > 0 {
		parts = append(parts, strings.Join(s.ToolOutputs, "\n---\n"))
	}
	if len(s.ToolErrors) > 0 {
		parts = append(parts, "Tool errors:\n"+strings.Join(s.ToolErrors, "\n"))
	}
	if s.FatalError != "" {
		parts = append(parts, "Fatal error: "+s.FatalError)
	}
	if len(parts) == 0 {
		return "(no output)"
	}
	return strings.Join(parts, "\n\n")
}

// Done returns true when the session completed without a fatal error.
func (s *StreamSummary) Done() bool {
	return s.FatalError == ""
}

func parseLine(b []byte) (*Event, error) {
	var ev Event
	if err := json.Unmarshal(b, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}
