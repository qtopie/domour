package provider

import (
	"context"
	"strings"
	"time"
)

type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (m *MockProvider) Name() string {
	return "mock"
}

func (m *MockProvider) Generate(ctx context.Context, req GenerateRequest) (<-chan Chunk, error) {
	out := make(chan Chunk)
	go func() {
		defer close(out)
		text := "[mock] " + strings.TrimSpace(req.Content)
		if text == "[mock]" {
			text = "[mock] hello"
		}
		for _, part := range strings.Split(text, " ") {
			select {
			case <-ctx.Done():
				out <- Chunk{Done: true}
				return
			case <-time.After(20 * time.Millisecond):
				out <- Chunk{Text: part + " "}
			}
		}
		out <- Chunk{Done: true}
	}()
	return out, nil
}
