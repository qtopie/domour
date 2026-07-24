package agent

import (
	"context"
	"testing"
)

func TestLLMAgent(t *testing.T) {
	ag := NewLLMAgent("assistant", WithProvider("gemini"), WithModel("gemini-2.5-flash"))
	if ag.Name() != "assistant" {
		t.Errorf("expected name 'assistant', got %q", ag.Name())
	}

	ctx := context.Background()
	out, err := ag.Run(ctx, &Input{Message: "hello"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out.Content != "hello" || out.Provider != "gemini" {
		t.Errorf("unexpected output: %+v", out)
	}

	var streamContent string
	_, err = ag.Stream(ctx, &Input{Message: "streaming test"}, func(ev *StreamEvent) error {
		streamContent += ev.Content
		return nil
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if streamContent != "streaming test" {
		t.Errorf("expected 'streaming test', got %q", streamContent)
	}
}
