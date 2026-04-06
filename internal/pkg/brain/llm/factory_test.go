package llm

import "testing"

func TestNewChatModelSupportsOllama(t *testing.T) {
	t.Parallel()

	model, err := NewChatModel(t.Context(), &Config{
		Provider: "ollama",
		Model:    "phi4-mini",
	})
	if err != nil {
		t.Fatalf("NewChatModel(ollama) error = %v", err)
	}
	if model == nil {
		t.Fatal("NewChatModel(ollama) returned nil model")
	}
}

func TestNewChatModelSupportsOllamaWithCustomBaseURL(t *testing.T) {
	t.Parallel()

	model, err := NewChatModel(t.Context(), &Config{
		Provider: "ollama",
		Model:    "gemma3:4b",
		BaseURL:  "http://localhost:11434/v1",
		APIKey:   "local-token",
	})
	if err != nil {
		t.Fatalf("NewChatModel(ollama custom base url) error = %v", err)
	}
	if model == nil {
		t.Fatal("NewChatModel(ollama custom base url) returned nil model")
	}
}
