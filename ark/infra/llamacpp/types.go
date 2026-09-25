package llamacpp

import (
	"context"
	"errors"
)

// Sentinel errors.
var (
	ErrBackendClosed      = errors.New("llamacpp: engine is closed")
	ErrEmptyInput         = errors.New("llamacpp: prompt or tokens cannot be empty")
	ErrInvalidMarkerIndex = errors.New("llamacpp: marker index out of range")
	ErrEngineNotReady     = errors.New("llamacpp: engine is not ready")
)

// Config defines initialization parameters for the llama.cpp engine.
type Config struct {
	ModelPath string `json:"model_path"`
	NCtx      uint32 `json:"n_ctx"`
	NThreads  int32  `json:"n_threads"`
	UseStub   bool   `json:"use_stub"`
}

// ForwardRequest specifies tokens/prompt and marker positions where logits should be captured.
type ForwardRequest struct {
	Prompt        string  `json:"prompt,omitempty"`
	Tokens        []int32 `json:"tokens,omitempty"`
	MarkerIndices []int   `json:"marker_indices"` // 0-based token index positions
}

// ForwardResponse contains logits for each requested marker index.
type ForwardResponse struct {
	TokensCount int               `json:"tokens_count"`
	Logits      map[int][]float32 `json:"logits"` // key: marker index, value: vocab logits slice
}

// GenerateRequest defines an autoregressive generation task.
type GenerateRequest struct {
	Prompt      string   `json:"prompt"`
	MaxTokens   int      `json:"max_tokens"`
	Temperature float32  `json:"temperature"`
	TopP        float32  `json:"top_p,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// GenerateResponse holds the completed text output and token statistics.
type GenerateResponse struct {
	Content          string `json:"content"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	FinishReason     string `json:"finish_reason"`
}

// ChatMessage represents a single message in a conversational exchange.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest defines a conversational completion request.
type ChatRequest struct {
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float32       `json:"temperature"`
	TopP        float32       `json:"top_p,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
}

// ChatResponse holds the completed conversational response.
type ChatResponse struct {
	Message          ChatMessage `json:"message"`
	PromptTokens     int         `json:"prompt_tokens"`
	CompletionTokens int         `json:"completion_tokens"`
	FinishReason     string      `json:"finish_reason"`
}

// TokenChunk represents a streaming token emission.
type TokenChunk struct {
	Text    string `json:"text"`
	TokenID int32  `json:"token_id"`
	Done    bool   `json:"done"`
	Err     error  `json:"-"`
}

// Engine defines the llama.cpp runtime interface.
type Engine interface {
	// ForwardLogits executes a single forward pass without generation, extracting logits at marker positions.
	ForwardLogits(ctx context.Context, req ForwardRequest) (*ForwardResponse, error)

	// Generate synchronously generates a completion for the given prompt.
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)

	// Stream generates completion tokens asynchronously over a channel.
	Stream(ctx context.Context, req GenerateRequest) (<-chan TokenChunk, error)

	// Chat executes multi-turn conversational generation synchronously.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// ChatStream executes multi-turn conversational generation asynchronously with token streaming.
	ChatStream(ctx context.Context, req ChatRequest) (<-chan TokenChunk, error)

	// IsReady checks if the engine and model context are loaded and healthy.
	IsReady() bool

	// Close gracefully frees resources and stops background workers.
	Close() error
}
