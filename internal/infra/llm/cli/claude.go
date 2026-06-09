package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
)

type claudeProvider struct {
	command  string
	model    string
	proxyURL string
}

func (p *claudeProvider) GetGenerateArgs(ctx context.Context, prompt string, assetPaths []string, runtime *providerruntime.SessionRuntime) ([]string, error) {
	// Deterministically generate a valid UUID from session ID if not already valid
	var sessionID string
	if _, err := uuid.Parse(runtime.DomourSessionID); err == nil {
		sessionID = runtime.DomourSessionID
	} else if runtime.DomourSessionID != "" {
		sessionID = uuid.NewSHA1(uuid.NameSpaceDNS, []byte(runtime.DomourSessionID)).String()
	}

	args := []string{"--print", "--dangerously-skip-permissions"}

	if sessionID != "" {
		args = append(args, "--session-id", sessionID)
	}

	if runtime.Workspace != "" {
		args = append(args, "--add-dir", runtime.Workspace)
	}

	if p.model != "" {
		args = append(args, "--model", p.model)
	}

	if runtime.ConversationStarted {
		providerruntime.DefaultManager().MarkResume(runtime)
	}

	// Embed asset paths into prompt since claude CLI does not take assets as positional arguments
	if len(assetPaths) > 0 {
		prompt = prompt + "\n\n[Attached Assets/Images]:\n- " + strings.Join(assetPaths, "\n- ")
	}

	args = append(args, prompt)

	return args, nil
}

func (p *claudeProvider) HealthCheck(ctx context.Context) (string, error) {
	path, err := exec.LookPath(p.command)
	if err != nil {
		return "", fmt.Errorf("claude tool not found: %w", err)
	}
	return fmt.Sprintf("Claude tool is available: %s", path), nil
}
