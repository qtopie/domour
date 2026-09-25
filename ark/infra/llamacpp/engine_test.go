package llamacpp

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestEngine_ForwardLogits verifies [SPEC-LLAMA-001]
func TestEngine_ForwardLogits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	engine, err := NewEngine(Config{UseStub: true})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = engine.Close() }()

	req := ForwardRequest{
		Prompt:        "The quick brown fox jumps",
		MarkerIndices: []int{0, 2, 4},
	}

	resp, err := engine.ForwardLogits(ctx, req)
	if err != nil {
		t.Fatalf("ForwardLogits returned unexpected error: %v", err)
	}

	if resp == nil {
		t.Fatal("ForwardLogits returned nil response")
	}

	if resp.TokensCount != 5 {
		t.Errorf("expected TokensCount=5, got %d", resp.TokensCount)
	}

	if len(resp.Logits) != 3 {
		t.Fatalf("expected logits for 3 markers, got %d", len(resp.Logits))
	}

	for _, m := range []int{0, 2, 4} {
		logits, ok := resp.Logits[m]
		if !ok {
			t.Errorf("expected logits for marker %d", m)
			continue
		}
		if len(logits) == 0 {
			t.Errorf("expected non-empty logits vector for marker %d", m)
		}
	}
}

// TestEngine_ForwardLogits_InvalidMarker verifies [SPEC-LLAMA-002]
func TestEngine_ForwardLogits_InvalidMarker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	engine, err := NewEngine(Config{UseStub: true})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = engine.Close() }()

	// Prompt has 3 tokens (indices 0, 1, 2); index 5 is out of bounds
	req := ForwardRequest{
		Prompt:        "hello world foo",
		MarkerIndices: []int{0, 5},
	}

	_, err = engine.ForwardLogits(ctx, req)
	if err == nil {
		t.Fatal("expected error for out of bounds marker index, got nil")
	}

	if !errors.Is(err, ErrInvalidMarkerIndex) {
		t.Errorf("expected ErrInvalidMarkerIndex, got %v", err)
	}
}

// TestEngine_GenerateAndStream verifies [SPEC-LLAMA-003]
func TestEngine_GenerateAndStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	engine, err := NewEngine(Config{UseStub: true})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = engine.Close() }()

	// 1. Test Generate
	genReq := GenerateRequest{
		Prompt:      "Translate to French",
		MaxTokens:   5,
		Temperature: 0.5,
	}

	genResp, err := engine.Generate(ctx, genReq)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	if genResp == nil || genResp.Content == "" {
		t.Fatal("Generate returned empty response")
	}

	if genResp.CompletionTokens <= 0 {
		t.Errorf("expected CompletionTokens > 0, got %d", genResp.CompletionTokens)
	}

	// 2. Test Stream
	streamReq := GenerateRequest{
		Prompt:    "Streaming prompt",
		MaxTokens: 4,
	}

	streamCh, err := engine.Stream(ctx, streamReq)
	if err != nil {
		t.Fatalf("Stream returned unexpected error: %v", err)
	}

	var pieces []string
	var receivedDone bool

	for chunk := range streamCh {
		if chunk.Err != nil {
			t.Fatalf("Stream chunk had error: %v", chunk.Err)
		}
		if chunk.Done {
			receivedDone = true
		} else {
			pieces = append(pieces, chunk.Text)
		}
	}

	if !receivedDone {
		t.Error("Stream did not emit terminating chunk with Done: true")
	}

	if len(pieces) == 0 {
		t.Error("Stream did not emit any text chunks")
	}
}

// TestEngine_EmptyInputs verifies validation on empty prompt / tokens
func TestEngine_EmptyInputs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	engine, err := NewEngine(Config{UseStub: true})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = engine.Close() }()

	// ForwardLogits with empty prompt and tokens
	_, err = engine.ForwardLogits(ctx, ForwardRequest{Prompt: "   ", MarkerIndices: []int{0}})
	if !errors.Is(err, ErrEmptyInput) {
		t.Errorf("expected ErrEmptyInput on empty prompt, got %v", err)
	}

	// Generate with empty prompt
	_, err = engine.Generate(ctx, GenerateRequest{Prompt: "   "})
	if !errors.Is(err, ErrEmptyInput) {
		t.Errorf("expected ErrEmptyInput on empty generate prompt, got %v", err)
	}
}

// TestEngine_ClosedBackend verifies operations fail when engine is closed
func TestEngine_ClosedBackend(t *testing.T) {
	ctx := context.Background()

	engine, err := NewEngine(Config{UseStub: true})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if err := engine.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if engine.IsReady() {
		t.Error("expected IsReady to be false after Close")
	}

	_, err = engine.ForwardLogits(ctx, ForwardRequest{Prompt: "test", MarkerIndices: []int{0}})
	if !errors.Is(err, ErrBackendClosed) {
		t.Errorf("expected ErrBackendClosed, got %v", err)
	}

	_, err = engine.Generate(ctx, GenerateRequest{Prompt: "test"})
	if !errors.Is(err, ErrBackendClosed) {
		t.Errorf("expected ErrBackendClosed, got %v", err)
	}
}

// TestEngine_Chat verifies [SPEC-LLAMA-006]
func TestEngine_Chat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	engine, err := NewEngine(Config{UseStub: true})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = engine.Close() }()

	req := ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a concise assistant."},
			{Role: "user", Content: "Hello, who are you?"},
		},
		MaxTokens:   10,
		Temperature: 0.7,
		Stop:        []string{"<|im_end|>"},
	}

	resp, err := engine.Chat(ctx, req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Chat returned nil response")
	}

	if resp.Message.Role != "assistant" {
		t.Errorf("expected assistant role, got %q", resp.Message.Role)
	}

	if resp.Message.Content == "" {
		t.Error("expected non-empty message content")
	}

	if resp.CompletionTokens <= 0 {
		t.Errorf("expected positive CompletionTokens, got %d", resp.CompletionTokens)
	}
}

// TestEngine_ChatStream verifies [SPEC-LLAMA-007]
func TestEngine_ChatStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	engine, err := NewEngine(Config{UseStub: true})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = engine.Close() }()

	req := ChatRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: "Stream me a message"},
		},
		MaxTokens:   5,
		Temperature: 0.0,
	}

	streamCh, err := engine.ChatStream(ctx, req)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var chunks []TokenChunk
	for chunk := range streamCh {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
		chunks = append(chunks, chunk)
	}

	if len(chunks) == 0 {
		t.Fatal("expected chunks from ChatStream")
	}

	last := chunks[len(chunks)-1]
	if !last.Done {
		t.Errorf("expected last chunk to have Done: true, got %+v", last)
	}
}

