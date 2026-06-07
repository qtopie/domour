package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
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

type claudeAuthStatus struct {
	LoggedIn    bool   `json:"loggedIn"`
	AuthMethod  string `json:"authMethod"`
	APIProvider string `json:"apiProvider"`
}

func (p *claudeProvider) HealthCheck(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, p.command, "auth", "status", "--json")
	if p.proxyURL != "" {
		cmd.Env = append(os.Environ(),
			"HTTPS_PROXY="+p.proxyURL,
			"https_proxy="+p.proxyURL,
			"HTTP_PROXY="+p.proxyURL,
			"http_proxy="+p.proxyURL,
		)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to check auth status: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var status claudeAuthStatus
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		return "", fmt.Errorf("failed to parse auth status json: %w: %s", err, stdout.String())
	}

	if !status.LoggedIn {
		return "", fmt.Errorf("claude code is not logged in. please run: %s auth login", p.command)
	}

	return fmt.Sprintf("Authenticated (Method: %s, Provider: %s)", status.AuthMethod, status.APIProvider), nil
}
