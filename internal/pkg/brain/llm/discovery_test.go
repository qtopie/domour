package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverModelsOpenAICompatible(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"},{"id":"gpt-5"}]}`))
	}))
	defer server.Close()

	result, err := DiscoverModels(context.Background(), &Config{Provider: "openai", BaseURL: server.URL + "/v1"})
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if result.Provider != "openai" {
		t.Fatalf("Provider = %q, want openai", result.Provider)
	}
	if len(result.Models) != 2 || result.Models[0] != "gpt-4.1" || result.Models[1] != "gpt-5" {
		t.Fatalf("Models = %#v, want [gpt-4.1 gpt-5]", result.Models)
	}
}

func TestDiscoverModelsOllama(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("path = %q, want /api/tags", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"phi4-mini"},{"name":"llama3.2"}]}`))
	}))
	defer server.Close()

	result, err := DiscoverModels(context.Background(), &Config{Provider: "ollama", BaseURL: server.URL + "/v1"})
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(result.Models) != 2 || result.Models[0] != "llama3.2" || result.Models[1] != "phi4-mini" {
		t.Fatalf("Models = %#v, want sorted ollama models", result.Models)
	}
}

func TestDiscoverModelsGemini(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Fatalf("path = %q, want /v1beta/models", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "gem-key" {
			t.Fatalf("key = %q, want gem-key", r.URL.Query().Get("key"))
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-2.5-pro","supportedGenerationMethods":["generateContent"]},{"name":"models/text-embedding-004","supportedGenerationMethods":["embedContent"]}]}`))
	}))
	defer server.Close()

	result, err := DiscoverModels(context.Background(), &Config{Provider: "gemini", BaseURL: server.URL, APIKey: "gem-key"})
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(result.Models) != 1 || result.Models[0] != "gemini-2.5-pro" {
		t.Fatalf("Models = %#v, want [gemini-2.5-pro]", result.Models)
	}
}

func TestDiscoverModelsCLIUnsupported(t *testing.T) {
	t.Parallel()

	_, err := DiscoverModels(context.Background(), &Config{Provider: "github-copilot-cli"})
	if !errors.Is(err, ErrModelDiscoveryUnsupported) {
		t.Fatalf("DiscoverModels() error = %v, want ErrModelDiscoveryUnsupported", err)
	}
}
