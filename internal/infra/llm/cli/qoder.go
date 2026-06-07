package cli

import (
	"context"

	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
)

type qoderProvider struct {
	command  string
	model    string
	proxyURL string
}

func (p *qoderProvider) GetGenerateArgs(ctx context.Context, prompt string, assetPaths []string, runtime *providerruntime.SessionRuntime) ([]string, error) {
	args := []string{"-p", prompt, "-f", "text", "-q", "--workspace", runtime.Workspace, "--dangerously-skip-permissions"}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	if runtime.ConversationStarted {
		args = append(args, "--continue")
		providerruntime.DefaultManager().MarkResume(runtime)
	}
	args = append(args, assetPaths...)
	return args, nil
}

func (p *qoderProvider) HealthCheck(ctx context.Context) (string, error) {
	return "", nil
}
