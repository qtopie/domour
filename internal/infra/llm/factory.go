package llm

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/qtopie/domour/internal/infra/llm/cli"
	// homa "github.com/qtopie/domour/internal/assistant/llm"
	"google.golang.org/genai"
)

type Config struct {
	Provider string // "openai", "ollama", "gemini", "qwen", "gemini-cli", "github-copilot-cli", "qodercli"
	APIKey   string
	BaseURL  string // Optional for providers like OpenAI
	Model    string // e.g. "gpt-4", "gemini-pro"
	ProxyURL string // Optional, supports "http://", "https://", "socks5://", "socks5h://"
	Debug    bool
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
		dsClient, err := newDeepSeekHTTPClient(cfg.ProxyURL)
		if err != nil {
			return nil, err
		}
		return newOpenAICompatibleChatModel(ctx, dsClient, deepseekCfg)
	case "ollama":
		ollamaCfg := *cfg
		if strings.TrimSpace(ollamaCfg.BaseURL) == "" {
			ollamaCfg.BaseURL = "http://127.0.0.1:11434/v1"
		}
		if strings.TrimSpace(ollamaCfg.APIKey) == "" {
			ollamaCfg.APIKey = "ollama"
		}
		return newOpenAICompatibleChatModel(ctx, httpClient, ollamaCfg)
	case "gemini":
		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:     cfg.APIKey,
			HTTPClient: httpClient,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create gemini client: %w", err)
		}
		c := &gemini.Config{
			Client: client,
			Model:  cfg.Model,
		}
		return gemini.NewChatModel(ctx, c)
	case "qwen":
		c := &qwen.ChatModelConfig{
			APIKey: cfg.APIKey,
			Model:  cfg.Model,
		}
		if httpClient != nil {
			c.HTTPClient = httpClient
		}
		return qwen.NewChatModel(ctx, c)
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

func newDeepSeekHTTPClient(proxyURLRaw string) (*http.Client, error) {
	baseClient, err := newHTTPClientWithProxy(proxyURLRaw)
	if err != nil {
		return nil, err
	}
	if baseClient == nil {
		baseClient = &http.Client{}
	}
	transport := baseClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	baseClient.Transport = &deepseekInterceptorRoundTripper{underlay: transport}
	return baseClient, nil
}

type deepseekInterceptorRoundTripper struct {
	underlay http.RoundTripper
}

func (rt *deepseekInterceptorRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.underlay.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if strings.Contains(req.URL.Path, "/chat/completions") && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		resp.Body = &sseInterceptorReader{
			underlay: resp.Body,
			reader:   bufio.NewReader(resp.Body),
		}
	}
	return resp, nil
}

type sseInterceptorReader struct {
	underlay   io.ReadCloser
	reader     *bufio.Reader
	buf        bytes.Buffer
	isThinking bool
}

func (r *sseInterceptorReader) Read(p []byte) (n int, err error) {
	if r.buf.Len() > 0 {
		return r.buf.Read(p)
	}

	line, err := r.reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return 0, err
	}

	rewritten := rewriteDeepSeekSSELine(line, &r.isThinking)
	r.buf.WriteString(rewritten)
	return r.buf.Read(p)
}

func (r *sseInterceptorReader) Close() error {
	return r.underlay.Close()
}

func rewriteDeepSeekSSELine(line string, isThinking *bool) string {
	if !strings.HasPrefix(line, "data: ") {
		return line
	}
	if idx := strings.Index(line, "\"reasoning_content\":\""); idx != -1 {
		if !*isThinking {
			*isThinking = true
			line = strings.Replace(line, "\"reasoning_content\":\"", "\"content\":\"<think>", 1)
		} else {
			line = strings.Replace(line, "\"reasoning_content\":\"", "\"content\":\"", 1)
		}
	} else if strings.Contains(line, "\"content\":\"") {
		if *isThinking {
			*isThinking = false
			line = strings.Replace(line, "\"content\":\"", "\"content\":\"</think>", 1)
		}
	}
	return line
}
