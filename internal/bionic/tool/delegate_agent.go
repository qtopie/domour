package tool

import "context"

// DelegateTask is a structured request to an external agent.
type DelegateTask struct {
	// Task is the natural language description of what to do.
	Task string

	// SessionID is an optional session identifier. When provided, the external agent
	// will attempt to resume or continue a session with this ID. When empty, the
	// agent creates a new session. The session ID used is returned in DelegateResult.
	SessionID string

	// WorkDir is the working directory in which the external agent should operate.
	// If empty, the current process working directory is used.
	WorkDir string

	// Meta carries additional agent-specific parameters (e.g. model, veto level).
	Meta map[string]string
}

// DelegateResult is the structured output from an external agent delegation.
type DelegateResult struct {
	// SessionID is the session ID that was used (or created) during this delegation.
	// The caller can pass it back for a follow-up DelegateTask to continue the session.
	SessionID string

	// Observation is the full text output from the agent, suitable for returning
	// to the Cerebellum as a tool result observation.
	Observation string

	// Done is true when the agent completed successfully without a fatal error.
	Done bool

	// Meta contains supplementary information (e.g. token counts, tool names used).
	Meta map[string]string
}

// ExternalAgent is the uniform interface used by the Cerebellum to delegate tasks
// to external coding agents (Copilot CLI, Claude Code, Gemini CLI, etc.).
type ExternalAgent interface {
	// Name returns the agent's identifier (e.g. "copilot", "claude", "gemini").
	Name() string

	// Delegate sends a task to the external agent and waits for a result.
	Delegate(ctx context.Context, task DelegateTask) (DelegateResult, error)

	// Close releases any long-lived resources held by the agent (e.g. hook servers).
	Close(ctx context.Context) error
}
