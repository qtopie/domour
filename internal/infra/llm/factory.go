package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/qtopie/domour/internal/infra/llm/cli"
	"github.com/qtopie/domour/internal/infra/llm/runtime"
)

type Config struct {
	Provider string // "openai", "llamacpp", "gemini", "qwen", "gemini-cli", "github-copilot-cli", "qodercli"
	APIKey   string
	BaseURL  string // Optional for providers like OpenAI
	Model    string // e.g. "gpt-4", "gemini-pro"
	ProxyURL string // Optional, supports "http://", "https://", "socks5://", "socks5h://"
	Debug    bool
}

type sessionHeaderTransport struct {
	inner http.RoundTripper
}

func (s *sessionHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rmeta := runtime.RequestMetadataFromContext(req.Context())
	if rmeta.SessionID != "" {
		// Clone request to avoid mutating original request in RoundTripper
		req2 := new(http.Request)
		*req2 = *req
		req2.Header = make(http.Header, len(req.Header))
		for k, s := range req.Header {
			req2.Header[k] = append([]string(nil), s...)
		}
		req2.Header.Set("X-Session-ID", rmeta.SessionID)
		req = req2
	}
	return s.inner.RoundTrip(req)
}

// NewChatModel creates a new LLM instance based on the provider config.
func NewChatModel(ctx context.Context, cfg *Config) (model.ChatModel, error) {
	httpClient, err := newHTTPClientWithProxy(cfg.ProxyURL)
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	// case "homa":
	// 	return homa.NewHomaChatModel(&homa.HomaChatModelConfig{
	// 		APIKey: cfg.APIKey,
	// 	})
	case "agy-sdk", "agy_sdk", "antigravity-sdk":
		return cli.New(&cli.Config{
			Provider: "agy-sdk",
			Command:  "agy",
			Model:    cfg.Model,
			ProxyURL: cfg.ProxyURL,
			BaseURL:  cfg.BaseURL,
			Debug:    cfg.Debug,
		})
	case "agy-cli", "agy_cli", "agy":
		return cli.New(&cli.Config{
			Provider: "agy-cli",
			Command:  "agy",
			Model:    cfg.Model,
			ProxyURL: cfg.ProxyURL,
			Debug:    cfg.Debug,
		})
	case "gemini-cli", "gemini_cli":
		return cli.New(&cli.Config{
			Provider: "gemini-cli",
			Command:  "gemini",
			Model:    cfg.Model,
			ProxyURL: cfg.ProxyURL,
			Debug:    cfg.Debug,
		})
	case "github-copilot-cli", "copilot-cli", "github-copilot":
		return cli.New(&cli.Config{
			Provider: "github-copilot-cli",
			Command:  "copilot",
			Model:    cfg.Model,
			ProxyURL: cfg.ProxyURL,
			Debug:    cfg.Debug,
		})
	case "qodercli", "qoder-cli":
		return cli.New(&cli.Config{
			Provider: "qodercli",
			Command:  "qodercli",
			Model:    cfg.Model,
			ProxyURL: cfg.ProxyURL,
			Debug:    cfg.Debug,
		})
	case "claude", "claude-code":
		return cli.New(&cli.Config{
			Provider: "claude",
			Command:  "claude-code",
			Model:    cfg.Model,
			ProxyURL: cfg.ProxyURL,
			Debug:    cfg.Debug,
		})
	case "azure", "azure-openai", "azure_openai":
		azureCfg := *cfg
		apiVersion := "2024-06-01" // default stable version
		
		if u, err := url.Parse(azureCfg.BaseURL); err == nil && u.Host != "" {
			if q := u.Query(); q.Get("api-version") != "" {
				apiVersion = q.Get("api-version")
			}
			azureCfg.BaseURL = u.Scheme + "://" + u.Host
		}
		
		c := &openai.ChatModelConfig{
			APIKey:     azureCfg.APIKey,
			Model:      azureCfg.Model,
			ByAzure:    true,
			BaseURL:    azureCfg.BaseURL,
			APIVersion: apiVersion,
		}
		if httpClient != nil {
			c.HTTPClient = httpClient
		}
		return openai.NewChatModel(ctx, c)
	case "openai":
		return newOpenAICompatibleChatModel(ctx, httpClient, *cfg)
	case "deepseek":
		deepseekCfg := *cfg
		if strings.TrimSpace(deepseekCfg.BaseURL) == "" {
			deepseekCfg.BaseURL = "https://api.deepseek.com"
		}
		return newOpenAICompatibleChatModel(ctx, httpClient, deepseekCfg)
	case "llamacpp", "llama.cpp", "llama_cpp":
		llamacppCfg := *cfg
		if strings.TrimSpace(llamacppCfg.BaseURL) == "" {
			llamacppCfg.BaseURL = "http://127.0.0.1:8082/v1"
		}
		if strings.TrimSpace(llamacppCfg.APIKey) == "" {
			llamacppCfg.APIKey = "llamacpp"
		}
		// Handle remote node routing if model has @peerID suffix
		if idx := strings.Index(llamacppCfg.Model, "@"); idx >= 0 {
			targetPeerID := llamacppCfg.Model[idx+1:]
			llamacppCfg.Model = llamacppCfg.Model[:idx]
			if targetPeerID != "" && targetPeerID != "local-node" && targetPeerID != "auto" {
				llamacppCfg.BaseURL = fmt.Sprintf("http://localhost:3500/v1.0/invoke/%s/method/v1", targetPeerID)
			}
		}
		// Wrap client transport only for llamacpp
		if httpClient == nil {
			httpClient = &http.Client{}
		}
		innerTrans := httpClient.Transport
		if innerTrans == nil {
			innerTrans = http.DefaultTransport
		}
		httpClient.Transport = &sessionHeaderTransport{inner: innerTrans}

		return newOpenAICompatibleChatModel(ctx, httpClient, llamacppCfg)
	case "qwen":
		c := &qwen.ChatModelConfig{
			APIKey: cfg.APIKey,
			Model:  cfg.Model,
		}
		if httpClient != nil {
			c.HTTPClient = httpClient
		}
		return qwen.NewChatModel(ctx, c)
	case "dapractor", "dapr-actor", "actor":
		return newDaprActorChatModel(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}

func newOpenAICompatibleChatModel(ctx context.Context, httpClient *http.Client, cfg Config) (model.ChatModel, error) {
	c := &openai.ChatModelConfig{
		APIKey: cfg.APIKey,
		Model:  cfg.Model,
	}
	if httpClient != nil {
		c.HTTPClient = httpClient
	}
	if cfg.BaseURL != "" {
		c.BaseURL = cfg.BaseURL
	}
	return openai.NewChatModel(ctx, c)
}

func newHTTPClientWithProxy(proxyURLRaw string) (*http.Client, error) {
	proxyURL, err := parseProxyURL(proxyURLRaw)
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		return nil, nil
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("failed to init proxy transport")
	}

	transport := defaultTransport.Clone()
	transport.Proxy = http.ProxyURL(proxyURL)

	return &http.Client{Transport: transport}, nil
}

func parseProxyURL(proxyURLRaw string) (*url.URL, error) {
	proxyURLRaw = strings.TrimSpace(proxyURLRaw)
	if proxyURLRaw == "" {
		return nil, nil
	}

	if strings.HasPrefix(proxyURLRaw, "sock5://") {
		proxyURLRaw = "socks5://" + strings.TrimPrefix(proxyURLRaw, "sock5://")
	}

	proxyURL, err := url.Parse(proxyURLRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url %q: %w", proxyURLRaw, err)
	}

	if proxyURL.Host == "" {
		return nil, fmt.Errorf("invalid proxy url %q: host is required", proxyURLRaw)
	}

	switch proxyURL.Scheme {
	case "http", "https", "socks5", "socks5h":
		return proxyURL, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q, only http/https/socks5/socks5h are supported", proxyURL.Scheme)
	}
}

