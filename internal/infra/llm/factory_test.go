package llm

import "testing"

func TestNewChatModelSupportsLlamaCpp(t *testing.T) {
	t.Parallel()

	model, err := NewChatModel(t.Context(), &Config{
		Provider: "llamacpp",
		Model:    "llama-3.2-3b",
	})
	if err != nil {
		t.Fatalf("NewChatModel(llamacpp) error = %v", err)
	}
	if model == nil {
		t.Fatal("NewChatModel(llamacpp) returned nil model")
	}
}

func TestNewChatModelSupportsLlamaCppWithCustomBaseURL(t *testing.T) {
	t.Parallel()

	model, err := NewChatModel(t.Context(), &Config{
		Provider: "llamacpp",
		Model:    "gemma-3-4b-it-Q4_K_M",
		BaseURL:  "http://localhost:8080/v1",
		APIKey:   "local-token",
	})
	if err != nil {
		t.Fatalf("NewChatModel(llamacpp custom base url) error = %v", err)
	}
	if model == nil {
		t.Fatal("NewChatModel(llamacpp custom base url) returned nil model")
	}
}

func TestNewChatModelSupportsLlamaCppAliases(t *testing.T) {
	t.Parallel()

	for _, alias := range []string{"llama.cpp", "llama_cpp"} {
		model, err := NewChatModel(t.Context(), &Config{
			Provider: alias,
			Model:    "qwen2.5-7b",
		})
		if err != nil {
			t.Fatalf("NewChatModel(%s) error = %v", alias, err)
		}
		if model == nil {
			t.Fatalf("NewChatModel(%s) returned nil model", alias)
		}
	}
}

func TestNewChatModelSupportsDeepSeek(t *testing.T) {
	t.Parallel()

	model, err := NewChatModel(t.Context(), &Config{
		Provider: "deepseek",
		Model:    "deepseek-chat",
		APIKey:   "fake-key",
	})
	if err != nil {
		t.Fatalf("NewChatModel(deepseek) error = %v", err)
	}
	if model == nil {
		t.Fatal("NewChatModel(deepseek) returned nil model")
	}
}
