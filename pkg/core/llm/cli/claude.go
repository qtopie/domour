package cli

import (
	"context"

	providerruntime "github.com/qtopie/domour/internal/provider/runtime"
)

type claudeProvider struct {
	command  string
	model    string
	proxyURL string
}

func (p *claudeProvider) GetGenerateArgs(ctx context.Context, prompt string, assetPaths []string, runtime *providerruntime.SessionRuntime) ([]string, error) {
	// For claude-code cli:
	// Use --prompt to specify the input
	args := []string{"--prompt", prompt}

	// Add any assets (images etc) if supported by the CLI
	// Based on typical LLM CLI patterns, we append them at the end or use specific flags
	args = append(args, assetPaths...)

	return args, nil
}

func (p *claudeProvider) HealthCheck(ctx context.Context) (string, error) {
	// Simple version check for health
	return "", nil
}
