package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

var ErrModelDiscoveryUnsupported = errors.New("model discovery is not supported for this provider")

type DiscoveryResult struct {
	Provider string
	Models   []string
	Source   string
}

func DiscoverModels(ctx context.Context, cfg *Config) (DiscoveryResult, error) {
	if cfg == nil {
		return DiscoveryResult{}, fmt.Errorf("llm config is required")
	}

	provider := normalizeDiscoveryProvider(cfg.Provider)
	switch provider {
	case "ollama":
		return discoverOllamaModels(ctx, *cfg)
	case "openai":
		return discoverOpenAIModels(ctx, *cfg)
	case "gemini":
		return discoverGeminiModels(ctx, *cfg)
	case "github-copilot-cli", "qodercli", "agy-cli", "agy-sdk":
		return DiscoveryResult{Provider: provider}, ErrModelDiscoveryUnsupported
	default:
		return DiscoveryResult{Provider: provider}, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}

func discoverOpenAIModels(ctx context.Context, cfg Config) (DiscoveryResult, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	endpoint := ensureModelsEndpoint(baseURL)
	models, err := fetchOpenAIModels(ctx, endpoint, cfg)
	if err != nil {
		return DiscoveryResult{}, err
	}
	return DiscoveryResult{
		Provider: "openai",
		Models:   models,
		Source:   endpoint,
	}, nil
}

func discoverOllamaModels(ctx context.Context, cfg Config) (DiscoveryResult, error) {
	rootURL := strings.TrimSpace(cfg.BaseURL)
	if rootURL == "" {
		rootURL = "http://127.0.0.1:11434/v1"
	}
	rootURL = trimTrailingVersionPath(rootURL)
	endpoint := strings.TrimRight(rootURL, "/") + "/api/tags"

	httpClient, err := newHTTPClientWithProxy(cfg.ProxyURL)
	if err != nil {
		return DiscoveryResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("build ollama discovery request: %w", err)
	}
	response, err := effectiveHTTPClient(httpClient).Do(request)
	if err == nil && response != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		defer response.Body.Close()
		var payload struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			return DiscoveryResult{}, fmt.Errorf("decode ollama models: %w", err)
		}
		models := make([]string, 0, len(payload.Models))
		for _, item := range payload.Models {
			models = append(models, item.Name)
		}
		return DiscoveryResult{
			Provider: "ollama",
			Models:   normalizeModelList(models),
			Source:   endpoint,
		}, nil
	}
	if response != nil {
		response.Body.Close()
	}

	fallbackEndpoint := ensureModelsEndpoint(strings.TrimRight(rootURL, "/") + "/v1")
	models, fallbackErr := fetchOpenAIModels(ctx, fallbackEndpoint, cfg)
	if fallbackErr != nil {
		if err != nil {
			return DiscoveryResult{}, fmt.Errorf("discover ollama models: %w; fallback %v", err, fallbackErr)
		}
		return DiscoveryResult{}, fallbackErr
	}
	return DiscoveryResult{
		Provider: "ollama",
		Models:   models,
		Source:   fallbackEndpoint,
	}, nil
}

func discoverGeminiModels(ctx context.Context, cfg Config) (DiscoveryResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return DiscoveryResult{}, fmt.Errorf("gemini model discovery requires an API key")
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	endpoint := baseURL + "/v1beta/models?key=" + url.QueryEscape(strings.TrimSpace(cfg.APIKey))

	httpClient, err := newHTTPClientWithProxy(cfg.ProxyURL)
	if err != nil {
		return DiscoveryResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("build gemini discovery request: %w", err)
	}
	response, err := effectiveHTTPClient(httpClient).Do(request)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("request gemini models: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DiscoveryResult{}, fmt.Errorf("request gemini models: unexpected status %d", response.StatusCode)
	}

	var payload struct {
		Models []struct {
			Name                       string   `json:"name"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return DiscoveryResult{}, fmt.Errorf("decode gemini models: %w", err)
	}

	models := make([]string, 0, len(payload.Models))
	for _, item := range payload.Models {
		if supportsGeneration(item.SupportedGenerationMethods) {
			models = append(models, strings.TrimPrefix(strings.TrimSpace(item.Name), "models/"))
		}
	}

	return DiscoveryResult{
		Provider: "gemini",
		Models:   normalizeModelList(models),
		Source:   baseURL + "/v1beta/models",
	}, nil
}

func fetchOpenAIModels(ctx context.Context, endpoint string, cfg Config) ([]string, error) {
	httpClient, err := newHTTPClientWithProxy(cfg.ProxyURL)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build model discovery request: %w", err)
	}
	if apiKey := strings.TrimSpace(cfg.APIKey); apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	response, err := effectiveHTTPClient(httpClient).Do(request)
	if err != nil {
		return nil, fmt.Errorf("request model discovery: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("request model discovery: unexpected status %d", response.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode model discovery response: %w", err)
	}

	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		models = append(models, item.ID)
	}
	return normalizeModelList(models), nil
}

func effectiveHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func normalizeDiscoveryProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gemini-cli", "gemini_cli":
		return "gemini"
	case "agy-sdk", "agy_sdk", "antigravity-sdk":
		return "agy-sdk"
	case "agy-cli", "agy_cli", "agy":
		return "agy-cli"
	case "github-copilot-cli", "copilot-cli", "github-copilot":
		return "github-copilot-cli"
	case "qodercli", "qoder-cli", "qoder":
		return "qodercli"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func ensureModelsEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch {
	case strings.HasSuffix(baseURL, "/models"):
		return baseURL
	case strings.HasSuffix(baseURL, "/v1"):
		return baseURL + "/models"
	default:
		return baseURL + "/v1/models"
	}
}

func trimTrailingVersionPath(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, suffix := range []string{"/v1", "/v1beta", "/v1alpha"} {
		if strings.HasSuffix(baseURL, suffix) {
			return strings.TrimSuffix(baseURL, suffix)
		}
	}
	return baseURL
}

func supportsGeneration(methods []string) bool {
	for _, method := range methods {
		switch strings.TrimSpace(method) {
		case "generateContent", "streamGenerateContent":
			return true
		}
	}
	return false
}

func normalizeModelList(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	sort.Strings(out)
	return out
}
