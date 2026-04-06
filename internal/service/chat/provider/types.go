package provider

import "context"

type Message struct {
	Role    string
	Content string
}

type GenerateRequest struct {
	SessionID      string
	ConversationID string
	SenderID       string
	Content        string
	History        []Message
	Context        map[string]any
}

type Chunk struct {
	Text string
	Done bool
	Err  error
}

type Provider interface {
	Name() string
	Generate(ctx context.Context, req GenerateRequest) (<-chan Chunk, error)
}
