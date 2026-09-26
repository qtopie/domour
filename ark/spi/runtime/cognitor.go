package runtime

import "context"

// Message represents a prompt message in a reasoning conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CognitorRequest represents parameters for calling a cognitive reasoning engine.
type CognitorRequest struct {
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// CognitorResponse represents the full non-streaming output from a Cognitor.
type CognitorResponse struct {
	Content    string `json:"content"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	TokenUsage int    `json:"token_usage,omitempty"`
}

// CognitorChunk represents a streaming event emitted by a Cognitor.
type CognitorChunk struct {
	Type    string `json:"type"` // "text" | "thought" | "error"
	Content string `json:"content"`
	Done    bool   `json:"done"`
	Err     error  `json:"-"`
}

// Cognitor is the unified abstraction for cognitive LLM execution engines.
// Both Primary Core and Secondary Core implement this interface:
// - Primary Core: Handles complex reasoning, multi-step planning, deep thinking, and heavy tool orchestration.
// - Secondary Core: Handles lightweight tasks, fast reflexive responses, rapid intent/output calibration,
//   quick error correction, safety guardrail checks, and background tasks (memory synthesis, OCR parsing).
type Cognitor interface {
	Generate(ctx context.Context, req *CognitorRequest) (*CognitorResponse, error)
	Stream(ctx context.Context, req *CognitorRequest) (<-chan CognitorChunk, error)
	IsReady(ctx context.Context) (bool, error)
}
