package cognitor

import (
	"context"
	"testing"

	"github.com/qtopie/domour/ark/session"
)

func TestBuildSchemaMessages(t *testing.T) {
	req := &Request{
		SystemPromptOverride: "You are a helpful assistant",
		History: []session.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		Message: "How are you?",
	}

	msgs, err := buildSchemaMessages(req)
	if err != nil {
		t.Fatalf("buildSchemaMessages failed: %v", err)
	}

	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}

	if msgs[0].Content != "You are a helpful assistant" {
		t.Errorf("unexpected system prompt: %s", msgs[0].Content)
	}
	if msgs[3].Content != "How are you?" {
		t.Errorf("unexpected final message: %s", msgs[3].Content)
	}
}

func TestNilRequest(t *testing.T) {
	c := &Client{}
	_, err := c.Generate(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request in Generate")
	}

	_, err = c.Stream(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request in Stream")
	}
}
