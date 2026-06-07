package provider

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// APIConfig holds the configuration for an API-based LLM connection.
type APIConfig struct {
	Provider string // e.g. "openai", "gemini", "deepseek", "ollama"
	APIKey   string
	BaseURL  string
	Model    string
	ProxyURL string
	Debug    bool
}

// APIProvider defines a unified contract for API-based LLM adapters.
type APIProvider interface {
	// ProviderName returns the name of this API provider (e.g. "openai")
	ProviderName() string

	// ModelName returns the model name being used
	ModelName() string

	// Underlay returns Eino's standard ChatModel interface representing this API model.
	Underlay() model.ChatModel

	// GenerateMessage invokes the API model to generate a message response.
	GenerateMessage(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error)

	// IsReady checks if the API service is ready and reachable.
	IsReady(ctx context.Context) (bool, error)
}