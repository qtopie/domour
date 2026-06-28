package copilot

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// SessionConfig controls how a Copilot CLI session is started.
type SessionConfig struct {
	// SessionID is the Copilot session ID. If empty, a new UUID is generated and
	// used with --session-id so it can be resumed later. If non-empty, the session
	// is started (or resumed) with this ID.
	SessionID string

	// Task is the prompt sent to Copilot CLI via -p.
	Task string

	// WorkDir is the working directory for the Copilot process.
	// Defaults to the current working directory.
	WorkDir string

	// AllowTools is a list of tool permission patterns passed via --allow-tool.
	// Example: []string{"shell(go:*)", "write"}
	AllowTools []string

	// DenyTools is a list of tool permission patterns passed via --deny-tool.
	// Example: []string{"shell(rm:*)"}
	DenyTools []string

	// AllowAllTools passes --allow-all-tools (equivalent to COPILOT_ALLOW_ALL=true).
	AllowAllTools bool

	// AllowAllPaths passes --allow-all-paths.
	AllowAllPaths bool

	// Model sets the AI model via --model.
	Model string

	// HookServerAddr is the "host:port" of the Domour hook server.
	// When set, COPILOT_HOOK_ALLOW_LOCALHOST=1 is injected automatically.
	HookServerAddr string

	// VetoLevel is only used by the parent delegate tool to construct
	// the VetoEngine; the SessionManager itself does not use it directly.
	VetoLevel VetoLevel

	// ExtraEnv contains additional environment variables for the subprocess.
	ExtraEnv []string

	// Binary is the path to the copilot binary.
	// If empty, "copilot" is resolved from PATH.
	Binary string
}

// Session represents an active (or completed) Copilot CLI subprocess.
type Session struct {
	ID     string
	config SessionConfig
	cmd    *exec.Cmd

	stdout io.ReadCloser
	stderr io.ReadCloser

	mu     sync.Mutex
	done   bool
	result *StreamSummary
	err    error
}

// SessionManager creates and tracks Copilot CLI sessions for the Cerebellum layer.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewSessionManager creates an empty SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

// Run starts a Copilot CLI session with the given config, streams its output,
// and returns the collected StreamSummary once the process exits.
//
// If cfg.SessionID is empty, a new UUID is assigned and stored in the returned
// Session so the caller can resume later.
func (m *SessionManager) Run(ctx context.Context, cfg SessionConfig) (*Session, *StreamSummary, error) {
	if strings.TrimSpace(cfg.Task) == "" {
		return nil, nil, fmt.Errorf("copilot session: task prompt is required")
	}

	if cfg.SessionID == "" {
		cfg.SessionID = uuid.New().String()
	}

	binary, err := resolveBinary(cfg.Binary)
	if err != nil {
		return nil, nil, err
	}

	args := buildArgs(cfg)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = buildEnv(cfg)

	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("copilot session: stdout pipe: %w", err)
	}
	// Merge stderr into a buffer for error reporting
	var stderrBuf strings.Builder
	stderrPipe, _ := cmd.StderrPipe()

	sess := &Session{
		ID:     cfg.SessionID,
		config: cfg,
		cmd:    cmd,
		stdout: stdout,
	}

	m.mu.Lock()
	m.sessions[cfg.SessionID] = sess
	m.mu.Unlock()

	slog.Info("Starting Copilot CLI session",
		"session_id", cfg.SessionID,
		"binary", binary,
		"args", maskSecrets(args),
		"veto_level", cfg.VetoLevel.String(),
	)

	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		delete(m.sessions, cfg.SessionID)
		m.mu.Unlock()
		return nil, nil, fmt.Errorf("copilot session: start process: %w", err)
	}

	// Drain stderr in background
	if stderrPipe != nil {
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				line := scanner.Text()
				slog.Debug("copilot stderr", "session", cfg.SessionID, "line", line)
				stderrBuf.WriteString(line + "\n")
			}
		}()
	}

	// Parse JSONL stdout
	parser := NewStreamParser(stdout)
	summary, parseErr := parser.Collect()

	// Wait for process to exit
	waitErr := cmd.Wait()

	m.mu.Lock()
	delete(m.sessions, cfg.SessionID)
	m.mu.Unlock()

	if parseErr != nil && parseErr != io.EOF {
		return sess, summary, fmt.Errorf("copilot session: stream parse error: %w", parseErr)
	}

	if waitErr != nil {
		errDetail := strings.TrimSpace(stderrBuf.String())
		slog.Warn("Copilot CLI process exited with error",
			"session", cfg.SessionID,
			"error", waitErr,
			"stderr", errDetail,
		)
		// Non-zero exit is not always fatal (agent may have finished with an error response)
		if summary.FatalError == "" && errDetail != "" {
			summary.FatalError = fmt.Sprintf("process exited: %v: %s", waitErr, errDetail)
		}
	}

	slog.Info("Copilot CLI session completed",
		"session_id", cfg.SessionID,
		"assistant_chars", len(summary.AssistantText),
		"tool_calls", len(summary.ToolOutputs)+len(summary.ToolErrors),
	)

	return sess, summary, nil
}

// Get returns a session by ID if it is still running.
func (m *SessionManager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// buildArgs constructs the copilot CLI argument list from a SessionConfig.
func buildArgs(cfg SessionConfig) []string {
	args := []string{
		"-p", cfg.Task,
		"--output-format", "json",
		"--session-id", cfg.SessionID,
		"--no-auto-update",
	}

	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}

	if cfg.AllowAllTools {
		args = append(args, "--allow-all-tools")
	} else {
		for _, t := range cfg.AllowTools {
			args = append(args, "--allow-tool="+t)
		}
		for _, t := range cfg.DenyTools {
			args = append(args, "--deny-tool="+t)
		}
	}

	if cfg.AllowAllPaths {
		args = append(args, "--allow-all-paths")
	}

	return args
}

// buildEnv constructs the environment for the Copilot subprocess.
func buildEnv(cfg SessionConfig) []string {
	env := make([]string, 0, len(os.Environ())+10)
	env = append(env, os.Environ()...)

	// Allow localhost HTTP hook callbacks (required for http:// preToolUse hooks).
	env = append(env, "COPILOT_HOOK_ALLOW_LOCALHOST=1")

	// If a hook server is set, ensure the env var is present.
	if cfg.HookServerAddr != "" {
		// Just ensure the above flag is present (already added).
		_ = cfg.HookServerAddr
	}

	env = append(env, cfg.ExtraEnv...)
	return env
}

// resolveBinary finds the copilot binary, checking the provided path first,
// then falling back to PATH lookup for "copilot" and "gh".
func resolveBinary(hint string) (string, error) {
	if hint != "" {
		if path, err := exec.LookPath(hint); err == nil {
			return path, nil
		}
		// Maybe it's an absolute path given directly
		if filepath.IsAbs(hint) {
			if _, err := os.Stat(hint); err == nil {
				return hint, nil
			}
		}
	}

	for _, name := range []string{"copilot", "gh"} {
		if path, err := exec.LookPath(name); err == nil {
			// "gh" is the GitHub CLI, not the Copilot standalone binary
			if name == "gh" {
				// We would need "gh copilot" subcommand which uses a different interface; skip for now.
				_ = path
				continue
			}
			return path, nil
		}
	}

	return "", fmt.Errorf("copilot binary not found in PATH; install from https://github.com/github/copilot-cli")
}

// maskSecrets removes -p / --prompt values from the args list for logging.
func maskSecrets(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, arg := range out {
		if (arg == "-p" || arg == "--prompt") && i+1 < len(out) {
			out[i+1] = "<redacted>"
		}
	}
	return out
}
