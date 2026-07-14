package proxy

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	appconfig "github.com/qtopie/domour/internal/config"
	brainllm "github.com/qtopie/domour/internal/infra/llm"
	"github.com/qtopie/domour/internal/infra/discovery"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Config struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
	ProxyURL string
	Debug    bool
}

type Response struct {
	Content  string
	Provider string
	Model    string
}


type TrustLevel string

const (
	TrustComplete TrustLevel = "complete" // 完全信任 (e.g. 本地模型)
	TrustGeneral  TrustLevel = "general"  // 一般信任
	TrustLow      TrustLevel = "low"      // 非隐私安全
)

type IntelligenceLevel string

const (
	IntelligenceLow    IntelligenceLevel = "low"    // 低智力 (通常更快)
	IntelligenceMedium IntelligenceLevel = "medium" // 中等智力
	IntelligenceHigh   IntelligenceLevel = "high"   // 高智力 (通常慢)
)

type ProviderMetadata struct {
	Type         string            // "cli" or "api"
	Trust        TrustLevel        // trust level
	Intelligence IntelligenceLevel // intelligence level
	Tags         map[string]string // custom tags
}

type ProviderFactory func(ctx context.Context, cfg Config) (model.ChatModel, error)

type RegistryEntry struct {
	Metadata ProviderMetadata
	Factory  ProviderFactory
}

type ProviderStatus struct {
	Healthy      bool
	LastCheck    time.Time
	LastCheckErr error
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]RegistryEntry)

	statusMu sync.RWMutex
	statuses = make(map[string]ProviderStatus)

	meter                = otel.Meter("github.com/qtopie/domour/internal/cognitor/proxy")
	providerHealthGauge, _ = meter.Int64Gauge("domour.provider.health", metric.WithDescription("Health status of the provider (1 for healthy, 0 for unhealthy)"))
	providerCheckCounter, _ = meter.Int64Counter("domour.provider.check_total", metric.WithDescription("Total number of provider health checks"))
	providerLatencyHist, _  = meter.Float64Histogram("domour.provider.check_latency", metric.WithDescription("Latency of provider health checks in seconds"))
)

// SetProviderHealth updates or registers the health status of a provider.
func SetProviderHealth(name string, healthy bool, err error) {
	statusMu.Lock()
	defer statusMu.Unlock()
	statuses[strings.ToLower(strings.TrimSpace(name))] = ProviderStatus{
		Healthy:      healthy,
		LastCheck:    time.Now(),
		LastCheckErr: err,
	}

	// Record metric
	var statusVal int64 = 0
	if healthy {
		statusVal = 1
	}
	providerHealthGauge.Record(context.Background(), statusVal, metric.WithAttributes(
		attribute.String("provider", strings.ToLower(strings.TrimSpace(name))),
	))
}

// IsProviderHealthy returns whether a provider is healthy.
// If a provider hasn't been checked yet, it defaults to true.
func IsProviderHealthy(name string) bool {
	statusMu.RLock()
	defer statusMu.RUnlock()
	status, exists := statuses[strings.ToLower(strings.TrimSpace(name))]
	if !exists {
		return true // Default to healthy until checked
	}
	return status.Healthy
}

// GetProviderHealthStatus returns the health status details for a provider.
func GetProviderHealthStatus(name string) (bool, error) {
	statusMu.RLock()
	defer statusMu.RUnlock()
	status, exists := statuses[strings.ToLower(strings.TrimSpace(name))]
	if !exists {
		return true, nil // Default to healthy until checked
	}
	return status.Healthy, status.LastCheckErr
}

// CheckAllProviders polls all registered providers once to update their health.
func CheckAllProviders(ctx context.Context) {
	registryMu.RLock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	registryMu.RUnlock()

	domourCfg, err := appconfig.LoadDomourConfig()

	for _, name := range names {
		// Only check providers that are currently in use or enabled
		if err == nil && !domourCfg.IsProviderInUse(name) {
			continue
		}
		start := time.Now()
		// Run health check with a short timeout to prevent hanging
		checkCtx, cancel := context.WithTimeout(ctx, 4*time.Second)

		var apiKey, baseURL, proxyURL string
		if err == nil {
			apiKey = domourCfg.APIKeyForProvider(name)
			baseURL = domourCfg.BaseURLForProvider(name)
			proxyURL = domourCfg.ProxyForProvider(name)
		}

		// Also allow environment variable fallbacks for the healthcheck
		if apiKey == "" {
			apiKey = firstNonEmpty(
				strings.TrimSpace(osEnv("DOMOUR_" + strings.ToUpper(name) + "_API_KEY")),
				strings.TrimSpace(osEnv("DOMOUR_DEFAULT_API_KEY")),
			)
		}
		if baseURL == "" {
			baseURL = firstNonEmpty(
				strings.TrimSpace(osEnv("DOMOUR_" + strings.ToUpper(name) + "_BASE_URL")),
				strings.TrimSpace(osEnv("DOMOUR_DEFAULT_BASE_URL")),
			)
		}

		cl, err := New(checkCtx, Config{
			Provider: name,
			Model:    "healthcheck",
			APIKey:   apiKey,
			BaseURL:  baseURL,
			ProxyURL: proxyURL,
		})
		if err != nil {
			SetProviderHealth(name, false, err)
			cancel()

			// Record metric
			providerCheckCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("provider", strings.ToLower(strings.TrimSpace(name))),
				attribute.String("status", "error"),
			))
			providerLatencyHist.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
				attribute.String("provider", strings.ToLower(strings.TrimSpace(name))),
			))
			continue
		}
		ready, readyErr := cl.IsReady(checkCtx)
		cancel()

		var status = "success"
		if !ready || readyErr != nil {
			status = "failed"
			errToSet := readyErr
			if errToSet == nil {
				errToSet = fmt.Errorf("provider %s is not ready", name)
			}
			SetProviderHealth(name, false, errToSet)
		} else {
			SetProviderHealth(name, true, nil)
		}

		// Record metric
		providerCheckCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("provider", strings.ToLower(strings.TrimSpace(name))),
			attribute.String("status", status),
		))
		providerLatencyHist.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("provider", strings.ToLower(strings.TrimSpace(name))),
		))
	}
}

// StartHeartbeat starts a background ticker to check provider health.
func StartHeartbeat(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		// Delay the initial check slightly to allow the config file to be
		// updated by cosmos-assistant (syncToDomour) before we first attempt
		// to validate API keys. Without this, the heartbeat fires before the
		// file watcher can propagate the correct API key, causing a spurious
		// 401 that marks the provider as unhealthy for up to 30 seconds.
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
		CheckAllProviders(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				CheckAllProviders(ctx)
			}
		}
	}()
}


// RegisterProvider dynamically registers a new LLM provider with its metadata/tags and factory.
func RegisterProvider(name string, meta ProviderMetadata, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[strings.ToLower(name)] = RegistryEntry{
		Metadata: meta,
		Factory:  factory,
	}
}

func init() {
	// Pre-register built-in API providers
	RegisterProvider("gemini", ProviderMetadata{
		Type:         "api",
		Trust:        TrustGeneral,
		Intelligence: IntelligenceHigh,
	}, defaultLLMFactory)

	RegisterProvider("openai", ProviderMetadata{
		Type:         "api",
		Trust:        TrustGeneral,
		Intelligence: IntelligenceHigh,
	}, defaultLLMFactory)

	RegisterProvider("deepseek", ProviderMetadata{
		Type:         "api",
		Trust:        TrustGeneral,
		Intelligence: IntelligenceHigh,
	}, defaultLLMFactory)

	RegisterProvider("llamacpp", ProviderMetadata{
		Type:         "api",
		Trust:        TrustComplete,
		Intelligence: IntelligenceMedium,
	}, defaultLLMFactory)

	RegisterProvider("dapr-actor", ProviderMetadata{
		Type:         "api",
		Trust:        TrustComplete,
		Intelligence: IntelligenceMedium,
	}, defaultLLMFactory)

	// Pre-register built-in CLI providers
	RegisterProvider("claude", ProviderMetadata{
		Type:         "cli",
		Trust:        TrustGeneral,
		Intelligence: IntelligenceHigh,
	}, defaultLLMFactory)

	RegisterProvider("gemini-cli", ProviderMetadata{
		Type:         "cli",
		Trust:        TrustGeneral,
		Intelligence: IntelligenceHigh,
	}, defaultLLMFactory)

	RegisterProvider("github-copilot-cli", ProviderMetadata{
		Type:         "cli",
		Trust:        TrustGeneral,
		Intelligence: IntelligenceHigh,
	}, defaultLLMFactory)

	RegisterProvider("qodercli", ProviderMetadata{
		Type:         "cli",
		Trust:        TrustComplete,
		Intelligence: IntelligenceLow,
	}, defaultLLMFactory)

	RegisterProvider("agy-cli", ProviderMetadata{
		Type:         "cli",
		Trust:        TrustComplete,
		Intelligence: IntelligenceMedium,
	}, defaultLLMFactory)

	RegisterProvider("agy-sdk", ProviderMetadata{
		Type:         "cli",
		Trust:        TrustComplete,
		Intelligence: IntelligenceMedium,
	}, defaultLLMFactory)

	// Start active health polling loop (heartbeat)
	StartHeartbeat(context.Background(), 30*time.Second)

	// When the config file changes (e.g. cosmos-assistant writes a new API key),
	// immediately re-check all providers so they are not stuck as unhealthy
	// until the next 30-second heartbeat tick.
	appconfig.WatchConfig(func() {
		go CheckAllProviders(context.Background())
	})
}

func defaultLLMFactory(ctx context.Context, cfg Config) (model.ChatModel, error) {
	return brainllm.NewChatModel(ctx, &brainllm.Config{
		Provider: cfg.Provider,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
		Model:    cfg.Model,
		ProxyURL: cfg.ProxyURL,
		Debug:    cfg.Debug,
	})
}

type Client struct {
	Type         string // "cli" or "api"
	provider     string
	model        string
	apiKey       string
	baseURL      string
	Trust        TrustLevel
	Intelligence IntelligenceLevel
	Tags         map[string]string
	Chat         model.ChatModel
}

func NewTestClient(provider, modelName string, chat model.ChatModel) *Client {
	var wrappedChat model.ChatModel
	if chat != nil {
		wrappedChat = &sanitizingChatModel{ChatModel: chat}
	}
	return &Client{
		Type:         "api",
		provider:     provider,
		model:        modelName,
		Trust:        TrustGeneral,
		Intelligence: IntelligenceHigh,
		Chat:         wrappedChat,
	}
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	pName := strings.ToLower(strings.TrimSpace(cfg.Provider))

	if cfg.Model != "healthcheck" && !IsProviderHealthy(pName) {
		statusMu.RLock()
		checkErr := statuses[pName].LastCheckErr
		statusMu.RUnlock()
		if checkErr == nil {
			checkErr = fmt.Errorf("provider %s is not ready", cfg.Provider)
		}
		return nil, fmt.Errorf("provider %s is unhealthy: %w", cfg.Provider, checkErr)
	}

	registryMu.RLock()
	entry, exists := registry[pName]
	registryMu.RUnlock()

	var client model.ChatModel
	var err error

	if exists && entry.Factory != nil {
		client, err = entry.Factory(ctx, cfg)
	} else {
		client, err = brainllm.NewChatModel(ctx, &brainllm.Config{
			Provider: cfg.Provider,
			APIKey:   cfg.APIKey,
			BaseURL:  cfg.BaseURL,
			Model:    cfg.Model,
			ProxyURL: cfg.ProxyURL,
			Debug:    cfg.Debug,
		})
	}
	if err != nil {
		return nil, err
	}

	// Resolve metadata
	cliType := "api"
	if isCLIProvider(cfg.Provider) {
		cliType = "cli"
	}
	trust := TrustGeneral
	intel := IntelligenceHigh
	var tags map[string]string

	if exists {
		if entry.Metadata.Type != "" {
			cliType = entry.Metadata.Type
		}
		if entry.Metadata.Trust != "" {
			trust = entry.Metadata.Trust
		}
		if entry.Metadata.Intelligence != "" {
			intel = entry.Metadata.Intelligence
		}
		tags = entry.Metadata.Tags
	}

	return &Client{
		Type:         cliType,
		provider:     strings.TrimSpace(cfg.Provider),
		model:        strings.TrimSpace(cfg.Model),
		apiKey:       strings.TrimSpace(cfg.APIKey),
		baseURL:      strings.TrimSpace(cfg.BaseURL),
		Trust:        trust,
		Intelligence: intel,
		Tags:         tags,
		Chat:         &sanitizingChatModel{ChatModel: client},
	}, nil
}

func NewForEntry(ctx context.Context, entry string) (*Client, error) {
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
		Debug: domourCfg.IsDebug(),
	}
}

func (c *Client) Provider() string {
	return c.provider
}

func (c *Client) Model() string {
	return c.model
}

func (c *Client) IsReady(ctx context.Context) (bool, error) {
	// If it's a CLI-based model, use its specialized IsReady check
	if cliModel, ok := c.Chat.(interface {
		IsReady(context.Context) (bool, error)
	}); ok {
		return cliModel.IsReady(ctx)
	}

	// For API providers, attempt model discovery as a health check
	_, err := brainllm.DiscoverModels(ctx, &brainllm.Config{
		Provider: c.provider,
		APIKey:   c.apiKey,
		BaseURL:  c.baseURL,
	})
	if err == nil {
		return true, nil
	}

	if strings.Contains(err.Error(), "unsupported provider") || strings.Contains(err.Error(), "not supported") {
		return true, nil // Treat as ready if discovery is just not implemented
	}

	return false, err
}

func (c *Client) GenerateMessage(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	sanitizeMessages(messages)
	// Set a timeout of 30 seconds for the LLM execution if no timeout exists in the context
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var resp *schema.Message
	var err error

	// Try up to 2 times (initial attempt + exactly 1 retry)
	for i := 0; i < 2; i++ {
		// Respect context cancellation/timeout before execution
		if err := callCtx.Err(); err != nil {
			SetProviderHealth(c.provider, false, err)
			return nil, err
		}

		resp, err = c.Chat.Generate(callCtx, messages)
		if err == nil && resp != nil {
			break
		}

		if i == 0 {
			log.Printf("[Proxy] Retrying LLM call for provider %s due to error: %v", c.provider, err)
			select {
			case <-callCtx.Done():
				// Cancelled or timed out, do not retry
				err = callCtx.Err()
				break
			case <-time.After(500 * time.Millisecond):
				// Wait briefly before retrying
			}
		}
	}

	if err != nil {
		SetProviderHealth(c.provider, false, err)
		return nil, err
	}
	if resp == nil {
		err := fmt.Errorf("%s returned nil message", c.provider)
		SetProviderHealth(c.provider, false, err)
		return nil, err
	}

	// Passive health check: successful reply confirms connectivity.
	SetProviderHealth(c.provider, true, nil)
	discovery.Global().TouchStatus(fmt.Sprintf("%s:%s", c.provider, c.model))

	return resp, nil
}

func (c *Client) GenerateText(ctx context.Context, messages []*schema.Message) (Response, error) {
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

func (c *Client) BindTools(tools []*schema.ToolInfo) error {
	return c.Chat.BindTools(tools)
}

func isCLIProvider(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	return strings.HasSuffix(p, "-cli") ||
		strings.HasSuffix(p, "_cli") ||
		p == "claude" ||
		p == "claude-code" ||
		p == "github-copilot" ||
		p == "agy" ||
		p == "qodercli" ||
		p == "agy-sdk" ||
		p == "agy_sdk" ||
		p == "antigravity-sdk"
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

func sanitizeMessages(messages []*schema.Message) {
	for _, m := range messages {
		if m.Content == "" {
			m.Content = " "
		}
	}
}

type sanitizingChatModel struct {
	model.ChatModel
}

func (m *sanitizingChatModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	sanitizeMessages(in)
	return m.ChatModel.Generate(ctx, in, opts...)
}

func (m *sanitizingChatModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sanitizeMessages(in)
	return m.ChatModel.Stream(ctx, in, opts...)
}
