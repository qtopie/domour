package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	providerruntime "github.com/qtopie/domour/internal/provider/runtime"
)

type copilotProvider struct {
	command  string
	model    string
	proxyURL string
}

func (p *copilotProvider) GetGenerateArgs(ctx context.Context, prompt string, assetPaths []string, runtime *providerruntime.SessionRuntime) ([]string, error) {
	args := []string{"--prompt", prompt, "--allow-all", "--output-format", "text", "--silent", "--config-dir", runtime.ConfigDir}
	if p.model != "" {
		args = append([]string{"--model", p.model}, args...)
	}
	if runtime.ConversationStarted {
		args = append(args, "--continue")
		providerruntime.DefaultManager().MarkResume(runtime)
	}
	args = append(args, assetPaths...)
	return args, nil
}

func (p *copilotProvider) HealthCheck(ctx context.Context) (string, error) {
	health, err := p.GetQuotas(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("User: %s, Plan: %s, ChatEnabled: %v", health.User, health.Plan, health.ChatEnabled), nil
}

type CopilotQuota struct {
	User        string `json:"user"`
	Plan        string `json:"plan"`
	ChatEnabled bool   `json:"chat_enabled"`
	// Add other fields as needed
}

func (p *copilotProvider) GetQuotas(ctx context.Context) (*CopilotQuota, error) {
	token, err := p.getValidToken()
	if err != nil {
		return nil, err
	}

	url := "https://api.githubcopilot.com/copilot_internal/user"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot api returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var userResp struct {
		User        string `json:"user"`
		CopilotPlan string `json:"copilot_plan"`
		ChatEnabled bool   `json:"chat_enabled"`
	}
	if err := json.Unmarshal(body, &userResp); err != nil {
		return nil, err
	}

	return &CopilotQuota{
		User:        userResp.User,
		Plan:        userResp.CopilotPlan,
		ChatEnabled: userResp.ChatEnabled,
	}, nil
}

func (p *copilotProvider) getValidToken() (string, error) {
	// 1. Try to find GHU token from apps.json
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	appsPath := filepath.Join(home, ".config", "github-copilot", "apps.json")
	data, err := os.ReadFile(appsPath)
	if err != nil {
		return "", fmt.Errorf("failed to read copilot apps.json: %w", err)
	}

	var apps map[string]interface{}
	if err := json.Unmarshal(data, &apps); err != nil {
		return "", err
	}

	var ghuToken string
	for _, v := range apps {
		if m, ok := v.(map[string]interface{}); ok {
			if t, ok := m["oauth_token"].(string); ok && strings.HasPrefix(t, "ghu_") {
				ghuToken = t
				break
			}
		}
	}

	if ghuToken == "" {
		return "", fmt.Errorf("no valid copilot token (ghu_*) found in apps.json")
	}

	// 2. Exchange GHU token for session token
	exchangeURL := "https://api.github.com/copilot_internal/v2/token"
	req, err := http.NewRequest("GET", exchangeURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+ghuToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}

	var tokResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokResp); err != nil {
		return "", err
	}

	if tokResp.Token == "" {
		return "", fmt.Errorf("empty session token received")
	}

	return tokResp.Token, nil
}
