package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	providerruntime "github.com/qtopie/domour/internal/provider/runtime"
)

type agyProvider struct {
	command  string
	model    string
	proxyURL string
}

func (p *agyProvider) GetGenerateArgs(ctx context.Context, prompt string, assetPaths []string, runtime *providerruntime.SessionRuntime) ([]string, error) {
	// For agy, --prompt runs prompt non-interactively, and --dangerously-skip-permissions skips interactive prompts
	args := []string{"--prompt", prompt, "--dangerously-skip-permissions"}
	if runtime.Workspace != "" {
		args = append(args, "--add-dir", runtime.Workspace)
	}
	if runtime.ConversationStarted {
		args = append(args, "--continue")
		providerruntime.DefaultManager().MarkResume(runtime)
	}
	args = append(args, assetPaths...)
	return args, nil
}

func (p *agyProvider) HealthCheck(ctx context.Context) (string, error) {
	health, err := p.GetQuotas(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("RT: %v, Authenticated: %v, Network: %v", health.AvgRT, health.Authenticated, health.NetworkOK), nil
}

func (p *agyProvider) GetQuotas(ctx context.Context) (*GeminiAPIHealth, error) {
	creds, err := loadAgyOAuthCreds()
	if err != nil {
		return nil, fmt.Errorf("failed to load oauth creds: %w", err)
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	if p.proxyURL != "" {
		if proxy, err := url.Parse(p.proxyURL); err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxy),
			}
		}
	}

	startAssist := time.Now()
	assistData, err := p.callAPI(ctx, client, creds.AccessToken, "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", `{"mode":"HEALTH_CHECK"}`)
	rtAssist := time.Since(startAssist)
	if err != nil {
		return nil, fmt.Errorf("loadCodeAssist failed: %w", err)
	}

	var assistResp struct {
		CloudaicompanionProject string `json:"cloudaicompanionProject"`
		CurrentTier             struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"currentTier"`
	}
	if err := json.Unmarshal(assistData, &assistResp); err != nil {
		return nil, fmt.Errorf("failed to parse loadCodeAssist response: %w", err)
	}

	projectID := assistResp.CloudaicompanionProject
	if projectID == "" {
		return nil, fmt.Errorf("could not resolve project ID from loadCodeAssist")
	}

	startQuota := time.Now()
	quotaPayload := fmt.Sprintf(`{"project":"%s"}`, projectID)
	quotaData, err := p.callAPI(ctx, client, creds.AccessToken, "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota", quotaPayload)
	rtQuota := time.Since(startQuota)
	if err != nil {
		return nil, fmt.Errorf("retrieveUserQuota failed: %w", err)
	}

	var quotaResp struct {
		Buckets []GeminiQuotaBucket `json:"buckets"`
	}
	if err := json.Unmarshal(quotaData, &quotaResp); err != nil {
		return nil, fmt.Errorf("failed to parse retrieveUserQuota response: %w", err)
	}

	health := &GeminiAPIHealth{
		LastCheck:     time.Now(),
		AvgRT:         (rtAssist + rtQuota) / 2,
		Authenticated: true,
		NetworkOK:     true,
		ProjectID:     projectID,
		TierName:      assistResp.CurrentTier.Name,
		TierID:        assistResp.CurrentTier.ID,
		Quotas:        quotaResp.Buckets,
		RawLoadAssist: assistData,
		RawUserQuota:  quotaData,
	}

	return health, nil
}

func (p *agyProvider) callAPI(ctx context.Context, client *http.Client, token, url, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return io.ReadAll(resp.Body)
}

func loadAgyOAuthCreds() (*GeminiOAuthCreds, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	// Try loading from .antigravity/oauth_creds.json first, fallback to .gemini/oauth_creds.json
	path := filepath.Join(home, ".antigravity", "oauth_creds.json")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(home, ".gemini", "oauth_creds.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var creds GeminiOAuthCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	
	return &creds, nil
}
