package orchestrator

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// AgentInput represents the input parameters for executing an agent workflow.
type AgentInput struct {
	SessionID string                 `json:"session_id"`
	Stage     string                 `json:"stage,omitempty"`
	Provider  string                 `json:"provider,omitempty"`
	Model     string                 `json:"model,omitempty"`
	Messages  []*schema.Message      `json:"messages"`
	Tools     []*schema.ToolInfo     `json:"tools,omitempty"`
	Config    map[string]interface{} `json:"config,omitempty"`
}

// StreamEvent represents a single event during streaming agent execution.
type StreamEvent struct {
	Type    string          `json:"type"` // "chunk", "tool_call", "tool_result", "thought", "error"
	Content string          `json:"content,omitempty"`
	Message *schema.Message `json:"message,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// AgentRunner is the unified interface for running agents across different runtimes.
type AgentRunner interface {
	// Run synchronously executes an agent workflow to completion.
	Run(ctx context.Context, input *AgentInput) (*schema.Message, error)

	// Stream streams execution events (tokens, thoughts, tool steps) during agent execution.
	Stream(ctx context.Context, input *AgentInput, yield func(event *StreamEvent) error) (*schema.Message, error)
}
