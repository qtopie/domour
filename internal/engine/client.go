package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	appconfig "github.com/qtopie/domour/internal/config"
)

// CognitorClient is the LLM gateway interface, provided by the Diencephalon layer.
type CognitorClient interface {
	GetClient(ctx context.Context, entry string) (*proxy.Client, error)
	GetClientWithOverride(ctx context.Context, entry string, provider, model string) (*proxy.Client, error)
}

// ExecutorClient is the physical execution interface, provided by the Brainstem layer.
type ExecutorClient interface {
	Execute(ctx context.Context, command tool.Command) (tool.Result, error)
	Veto(ctx context.Context, action string) bool
	ListTools(ctx context.Context) ([]tool.ToolInfo, error)
	ToolManager() *tool.Manager
}

// localCognitorClient implements CognitorClient using local configuration.
type localCognitorClient struct {
	chatModel      *proxy.Client
	copilotModel   *proxy.Client
	autopilotModel *proxy.Client

	mu           sync.Mutex
	dynamicCache map[string]*proxy.Client
}

// NewLocalCognitorClient constructs a new local CognitorClient.
// Chat model is required; copilot and autopilot are optional — they degrade
// gracefully when unavailable (e.g. copilot CLI not installed on device).
func NewLocalCognitorClient() (CognitorClient, error) {
	// Log resolved config for debugging — helps diagnose provider resolution issues
	// on remote devices where env vars or config may differ from expected.
	if cfg, err := appconfig.LoadDomourConfig(); err == nil {
		resolved := proxy.ResolveConfig("chat", cfg)
		log.Printf("[Cognitor] resolved chat config — provider=%q model=%q baseURL=%q",
			resolved.Provider, resolved.Model, resolved.BaseURL)
	}

	chatModel, err := proxy.NewForEntry(context.Background(), "chat")
	if err != nil {
		return nil, fmt.Errorf("init chat model: %w", err)
	}
	copilotModel, err := proxy.NewForEntry(context.Background(), "copilot")
	if err != nil {
		log.Printf("[Cognitor] copilot model unavailable (degraded): %v", err)
		copilotModel = nil
	}
	autopilotModel, err := proxy.NewForEntry(context.Background(), "autopilot")
	if err != nil {
		log.Printf("[Cognitor] autopilot model unavailable (degraded): %v", err)
		autopilotModel = nil
	}

	return &localCognitorClient{
		chatModel:      chatModel,
		copilotModel:   copilotModel,
		autopilotModel: autopilotModel,
		dynamicCache:   map[string]*proxy.Client{},
	}, nil
}

func (b *localCognitorClient) GetClient(ctx context.Context, entry string) (*proxy.Client, error) {
	switch strings.ToLower(strings.TrimSpace(entry)) {
	case "chat":
		return b.chatModel, nil
	case "copilot":
		if b.copilotModel == nil {
			return nil, fmt.Errorf("copilot model is not available (degraded)")
		}
		return b.copilotModel, nil
	case "autopilot":
		if b.autopilotModel == nil {
			return nil, fmt.Errorf("autopilot model is not available (degraded)")
		}
		return b.autopilotModel, nil
	default:
		return nil, fmt.Errorf("unsupported brain entry %q", entry)
	}
}

func (b *localCognitorClient) GetClientWithOverride(ctx context.Context, entry string, provider, model string) (*proxy.Client, error) {
	entry = strings.ToLower(strings.TrimSpace(entry))

	// Resolve the base config for the entry from current settings/config
	domourCfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config for dynamic provider: %w", err)
	}

	build := func(ctx context.Context, e, p, m string) (*proxy.Client, error) {
		cfg := proxy.ResolveConfig(e, domourCfg)

		// Apply overrides
		if p != "" {
			cfg.Provider = p
			// When overriding provider, also load its key/url/proxy if defined in config
			cfg.APIKey = domourCfg.APIKeyForProvider(p)
			cfg.BaseURL = domourCfg.BaseURLForProvider(p)
			cfg.ProxyURL = domourCfg.ProxyForProvider(p)
			cfg.Model = domourCfg.ProviderModel(p)
		}
		if m != "" {
			cfg.Model = m
		}

		cacheKey := fmt.Sprintf("%s:%s:%s:%s", cfg.Provider, cfg.Model, cfg.APIKey, cfg.BaseURL)

		b.mu.Lock()
		defer b.mu.Unlock()
		if b.dynamicCache == nil {
			b.dynamicCache = make(map[string]*proxy.Client)
		}
		if client, ok := b.dynamicCache[cacheKey]; ok {
			return client, nil
		}
		newClient, err := proxy.New(ctx, cfg)
		if err != nil {
			return nil, err
		}
		b.dynamicCache[cacheKey] = newClient
		return newClient, nil
	}

	return resolveWithFallback(ctx, entry, provider, model, domourCfg, build, func(ctx context.Context, c *proxy.Client) (bool, error) {
		return c.IsReady(ctx)
	})
}

// localExecutorClient implements ExecutorClient using the local tool manager.
type localExecutorClient struct {
	manager *tool.Manager
}

// NewLocalExecutorClient constructs a new local ExecutorClient.
func NewLocalExecutorClient() (ExecutorClient, error) {
	manager, err := tool.NewDefaultManager()
	if err != nil {
		return nil, err
	}
	return &localExecutorClient{
		manager: manager,
	}, nil
}

func (m *localExecutorClient) Execute(ctx context.Context, command tool.Command) (tool.Result, error) {
	return m.manager.Execute(ctx, command)
}

func (m *localExecutorClient) Veto(ctx context.Context, action string) bool {
	return shouldRefuseOutput(action, "")
}

func (m *localExecutorClient) ListTools(ctx context.Context) ([]tool.ToolInfo, error) {
	return m.manager.List(), nil
}

func (m *localExecutorClient) ToolManager() *tool.Manager {
	return m.manager
}

func shouldRefuseOutput(prompt, content string) bool {
	value := strings.ToLower(strings.TrimSpace(prompt + "\n" + content))
	for _, marker := range []string{
		"jump off",
		"kill myself",
		"suicide",
		"hurt myself",
		"伤害自己",
		"自杀",
		"跳楼",
		"坠楼",
		"往下坠",
		"伤害他人",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

// reloadableCognitorClient wraps CognitorClient to support dynamic reloading.
type reloadableCognitorClient struct {
	mu    sync.RWMutex
	inner CognitorClient
}

// NewReloadableCognitorClient constructs a new reloadable CognitorClient.
// It never fails — if the initial client cannot be created (e.g. provider not
// yet running), it defers initialization until the first use via lazy init.
func NewReloadableCognitorClient() CognitorClient {
	inner, err := NewConfiguredCognitorClient()
	if err != nil {
		log.Printf("[Cognitor] Warning: initial cognitor client init failed (will retry on first use): %v", err)
		inner = nil
	}

	b := &reloadableCognitorClient{
		inner: inner,
	}

	appconfig.WatchConfig(func() {
		log.Printf("[Agent] Config change detected, reloading cognitor client...")
		newInner, err := NewConfiguredCognitorClient()
		if err != nil {
			log.Printf("[Agent] Failed to reload cognitor client: %v", err)
			return
		}
		b.mu.Lock()
		b.inner = newInner
		b.mu.Unlock()
		log.Printf("[Agent] Cognitor client reloaded successfully.")
	})

	return b
}

// NewConfiguredCognitorClient constructs CognitorClient configured via env and config.
func NewConfiguredCognitorClient() (CognitorClient, error) {
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return nil, err
	}

	mode := strings.ToLower(firstNonEmpty(
		strings.TrimSpace(os.Getenv("DOMOUR_BRAIN_MODE")),
		cfg.ServiceMode("brain"),
	))
	if mode == "dapr" {
		fmt.Println("[Proxy] Dapr brain mode requested. Dapr client has been moved out of engine runtime.")
	}
	return NewLocalCognitorClient()
}

func (b *reloadableCognitorClient) getInner() CognitorClient {
	b.mu.RLock()
	inner := b.inner
	b.mu.RUnlock()
	if inner != nil {
		return inner
	}
	// Lazy init: try to create the client now (provider may have started).
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inner != nil {
		return b.inner
	}
	newInner, err := NewConfiguredCognitorClient()
	if err != nil {
		log.Printf("[Cognitor] Lazy init failed (will retry on next call): %v", err)
		return nil
	}
	b.inner = newInner
	return b.inner
}

func (b *reloadableCognitorClient) GetClient(ctx context.Context, entry string) (*proxy.Client, error) {
	inner := b.getInner()
	if inner == nil {
		return nil, fmt.Errorf("cognitor client not initialized (provider may be unavailable)")
	}
	return inner.GetClient(ctx, entry)
}

func (b *reloadableCognitorClient) GetClientWithOverride(ctx context.Context, entry string, provider, model string) (*proxy.Client, error) {
	// GetClientWithOverride can always create a fresh client from config even
	// when inner is nil — provider-specific overrides bypass the cached clients.
	inner := b.getInner()
	if inner != nil {
		return inner.GetClientWithOverride(ctx, entry, provider, model)
	}
	// Inner still nil — build a one-shot client from config + overrides.
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return nil, fmt.Errorf("cognitor nil: failed to load config: %w", err)
	}

	build := func(ctx context.Context, e, p, m string) (*proxy.Client, error) {
		resolved := proxy.ResolveConfig(e, cfg)
		if p != "" {
			resolved.Provider = p
			resolved.APIKey = cfg.APIKeyForProvider(p)
			resolved.BaseURL = cfg.BaseURLForProvider(p)
			resolved.ProxyURL = cfg.ProxyForProvider(p)
			resolved.Model = cfg.ProviderModel(p)
		}
		if m != "" {
			resolved.Model = m
		}
		return proxy.New(ctx, resolved)
	}

	return resolveWithFallback(ctx, entry, provider, model, cfg, build, func(ctx context.Context, c *proxy.Client) (bool, error) {
		return c.IsReady(ctx)
	})
}

// NewConfiguredExecutorClient constructs ExecutorClient configured via env and config.
func NewConfiguredExecutorClient() (ExecutorClient, error) {
	cfg, err := appconfig.LoadDomourConfig()
	if err != nil {
		return nil, err
	}

	mode := strings.ToLower(firstNonEmpty(
		strings.TrimSpace(os.Getenv("DOMOUR_MOTOR_MODE")),
		cfg.ServiceMode("motor"),
	))
	if mode == "dapr" {
		fmt.Println("[Proxy] Dapr motor mode requested. Dapr client has been moved out of engine runtime.")
	}
	return NewLocalExecutorClient()
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
