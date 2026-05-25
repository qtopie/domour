package diencephalon

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	appconfig "github.com/qtopie/domour/internal/app/config"
	brainllm "github.com/qtopie/domour/pkg/core/llm"
	"github.com/qtopie/domour/pkg/core/registry"
)

type Config struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
	ProxyURL string
}

type Response struct {
	Content  string
	Provider string
	Model    string
}

type Client interface {
	Provider() string
	Model() string
	IsReady(ctx context.Context) (bool, error)
	GenerateMessage(ctx context.Context, messages []*schema.Message) (*schema.Message, error)
	GenerateText(ctx context.Context, messages []*schema.Message) (Response, error)
	BindTools(tools []*schema.ToolInfo) error
}

func (c *chatClient) IsReady(ctx context.Context) (bool, error) {
	// If it's a CLI-based model, use its specialized IsReady check
	if cliModel, ok := c.client.(interface {
		IsReady(context.Context) (bool, error)
	}); ok {
		return cliModel.IsReady(ctx)
	}

	// For API providers, attempt model discovery as a health check
	_, err := brainllm.DiscoverModels(ctx, &brainllm.Config{
		Provider: c.provider,
	})
	if err == nil {
		return true, nil
	}

	if strings.Contains(err.Error(), "unsupported provider") || strings.Contains(err.Error(), "not supported") {
		return true, nil // Treat as ready if discovery is just not implemented
	}

	return false, err
}

type chatClient struct {
	provider string
	model    string
	client   model.ChatModel
}

func New(ctx context.Context, cfg Config) (Client, error) {
	client, err := brainllm.NewChatModel(ctx, &brainllm.Config{
		Provider: cfg.Provider,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
		Model:    cfg.Model,
		ProxyURL: cfg.ProxyURL,
	})
	if err != nil {
		return nil, err
	}

	return &chatClient{
		provider: strings.TrimSpace(cfg.Provider),
		model:    strings.TrimSpace(cfg.Model),
		client:   client,
	}, nil
}

func NewForEntry(ctx context.Context, entry string) (Client, error) {
	domourCfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return nil, err
	}
	return New(ctx, ResolveConfig(entry, domourCfg))
}

func ResolveConfig(entry string, domourCfg appconfig.DomourConfig) Config {
	entry = strings.ToUpper(strings.TrimSpace(entry))
	entryKey := strings.ToLower(strings.TrimSpace(entry))

	provider := firstNonEmpty(
		entryEnv(entry, "PROVIDER"),
		strings.TrimSpace(osEnv("DOMOUR_DEFAULT_PROVIDER")),
		domourCfg.EntryProvider(entryKey),
		domourCfg.DefaultProviderName(),
	)
	if provider == "" {
		provider = "github-copilot-cli"
	}

	return Config{
		Provider: provider,
		APIKey: firstNonEmpty(
			entryEnv(entry, "API_KEY"),
			strings.TrimSpace(osEnv("DOMOUR_DEFAULT_API_KEY")),
			domourCfg.APIKeyForProvider(provider),
		),
		BaseURL: firstNonEmpty(
			entryEnv(entry, "BASE_URL"),
			strings.TrimSpace(osEnv("DOMOUR_DEFAULT_BASE_URL")),
			domourCfg.BaseURLForProvider(provider),
		),
		Model: firstNonEmpty(
			entryEnv(entry, "MODEL"),
			strings.TrimSpace(osEnv("DOMOUR_DEFAULT_MODEL")),
			domourCfg.EntryModel(entryKey),
			domourCfg.ProviderModel(provider),
			domourCfg.DefaultModelName(),
		),
		ProxyURL: firstNonEmpty(
			entryEnv(entry, "HTTPS_PROXY"),
			firstNonEmpty(
				strings.TrimSpace(osEnv("DOMOUR_DEFAULT_HTTPS_PROXY")),
				domourCfg.ProxyForProvider(provider),
			),
		),
	}
}

func (c *chatClient) Provider() string {
	return c.provider
}

func (c *chatClient) Model() string {
	return c.model
}

func (c *chatClient) GenerateMessage(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	resp, err := c.client.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("%s returned nil message", c.provider)
	}

	// Passive health check: successful reply confirms connectivity.
	// This will postpone the next active Discovery check.
	registry.Global().TouchStatus(fmt.Sprintf("%s:%s", c.provider, c.model))

	return resp, nil
}

func (c *chatClient) GenerateText(ctx context.Context, messages []*schema.Message) (Response, error) {
	resp, err := c.GenerateMessage(ctx, messages)
	if err != nil {
		return Response{}, err
	}

	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return Response{}, fmt.Errorf("%s returned empty content", c.provider)
	}

	return Response{
		Content:  content,
		Provider: c.provider,
		Model:    c.model,
	}, nil
}

func (c *chatClient) BindTools(tools []*schema.ToolInfo) error {
	return c.client.BindTools(tools)
}

var osEnv = func(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func entryEnv(entry, suffix string) string {
	entry = strings.TrimSpace(entry)
	suffix = strings.TrimSpace(suffix)
	if entry == "" || suffix == "" {
		return ""
	}
	return strings.TrimSpace(osEnv("DOMOUR_" + entry + "_" + suffix))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
