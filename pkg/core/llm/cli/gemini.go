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

type geminiProvider struct {
	command  string
	model    string
	proxyURL string
}

func (p *geminiProvider) GetGenerateArgs(ctx context.Context, prompt string, assetPaths []string, runtime *providerruntime.SessionRuntime) ([]string, error) {
	args := []string{"--prompt", prompt, "--output-format", "text", "--yolo", "--skip-trust"}
	if runtime.Workspace != "" {
		args = append(args, "--include-directories", runtime.Workspace)
	}
	if p.model != "" {
		args = append([]string{"--model", p.model}, args...)
	}
	if runtime.ConversationStarted {
		args = append(args, "--resume", "latest")
		providerruntime.DefaultManager().MarkResume(runtime)
	}
	args = append(args, assetPaths...)
	return args, nil
}

func (p *geminiProvider) HealthCheck(ctx context.Context) (string, error) {
	health, err := p.GetQuotas(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("RT: %v, Authenticated: %v, Network: %v", health.AvgRT, health.Authenticated, health.NetworkOK), nil
}

// Gemini specific types

type GeminiOAuthCreds struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiryDate   int64  `json:"expiry_date"`
	TokenType    string `json:"token_type"`
}

type GeminiQuotaBucket struct {
	ModelID           string    `json:"modelId"`
	RemainingFraction float64   `json:"remainingFraction"`
	ResetTime         time.Time `json:"resetTime"`
	TokenType         string    `json:"tokenType"`
}

type GeminiAPIHealth struct {
	LastCheck     time.Time           `json:"last_check"`
	AvgRT         time.Duration       `json:"avg_rt"`
	Authenticated bool                `json:"authenticated"`
	NetworkOK     bool                `json:"network_ok"`
	ProjectID     string              `json:"project_id"`
	TierName      string              `json:"tier_name"`
	TierID        string              `json:"tier_id"`
	Quotas        []GeminiQuotaBucket `json:"quotas"`
	
	RawLoadAssist json.RawMessage `json:"-"`
	RawUserQuota   json.RawMessage `json:"-"`
}

func (p *geminiProvider) GetQuotas(ctx context.Context) (*GeminiAPIHealth, error) {
	creds, err := loadGeminiOAuthCreds()
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

func (p *geminiProvider) callAPI(ctx context.Context, client *http.Client, token, url, body string) ([]byte, error) {
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

func loadGeminiOAuthCreds() (*GeminiOAuthCreds, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".gemini", "oauth_creds.json")
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
