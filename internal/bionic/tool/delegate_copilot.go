package tool

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/bionic/tool/copilot"
)

// CopilotAgent implements ExternalAgent by delegating tasks to GitHub Copilot CLI
// using the -p (non-interactive) mode with --output-format json. It intercepts
// all tool calls via the Domour Hook Server (preToolUse/postToolUse) for
// real-time Brainstem Veto enforcement and observability.
type CopilotAgent struct {
	hookServer *copilot.HookServer
	sessions   *copilot.SessionManager
	veto       *copilot.VetoEngine
}

// NewCopilotAgent creates a CopilotAgent with the given VetoEngine.
// Call Start() before using it.
func NewCopilotAgent(veto *copilot.VetoEngine) *CopilotAgent {
	return &CopilotAgent{
		veto:     veto,
		sessions: copilot.NewSessionManager(),
	}
}

// Start launches the embedded HTTP hook server and installs the Copilot hook config.
// Must be called before the first Delegate invocation.
func (a *CopilotAgent) Start(ctx context.Context) error {
	a.hookServer = copilot.NewHookServer(a.veto)
	return a.hookServer.Start(ctx)
}

// Name returns the agent identifier.
func (a *CopilotAgent) Name() string { return "copilot" }

// Delegate sends a task to Copilot CLI and returns the result.
func (a *CopilotAgent) Delegate(ctx context.Context, task DelegateTask) (DelegateResult, error) {
	if a.hookServer == nil {
		return DelegateResult{}, fmt.Errorf("CopilotAgent not started: call Start() first")
	}

	cfg := copilot.SessionConfig{
		SessionID:      task.SessionID,
		Task:           task.Task,
		WorkDir:        task.WorkDir,
		HookServerAddr: a.hookServer.Addr(),
		VetoLevel:      a.veto.Level,
	}

	// Pass through COPILOT_PROVIDER_* and COPILOT_MODEL env vars from parent
	// process so gh copilot can route through a custom API (e.g. DeepSeek).
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "COPILOT_PROVIDER_") ||
			strings.HasPrefix(e, "COPILOT_MODEL") ||
			strings.HasPrefix(e, "COPILOT_OFFLINE") {
			cfg.ExtraEnv = append(cfg.ExtraEnv, e)
		}
	}

	// Apply meta overrides
	if m := task.Meta; m != nil {
		if v := m["model"]; v != "" {
			cfg.Model = v
		}
		if v := m["allow_all_tools"]; v == "true" {
			cfg.AllowAllTools = true
		}
		if v := m["allow_all_paths"]; v == "true" {
			cfg.AllowAllPaths = true
		}
		if v := m["allow_tools"]; v != "" {
			cfg.AllowTools = splitComma(v)
		}
		if v := m["deny_tools"]; v != "" {
			cfg.DenyTools = splitComma(v)
		}
		if v := m["binary"]; v != "" {
			cfg.Binary = v
		}
	}

	sess, summary, err := a.sessions.Run(ctx, cfg)
	if err != nil {
		return DelegateResult{}, fmt.Errorf("copilot delegate: %w", err)
	}

	meta := map[string]string{
		"agent":      "copilot",
		"session_id": sess.ID,
		"veto_level": a.veto.Level.String(),
	}
	if summary.Tokens != nil {
		meta["tokens_prompt"] = fmt.Sprintf("%d", summary.Tokens.Prompt)
		meta["tokens_completion"] = fmt.Sprintf("%d", summary.Tokens.Completion)
		meta["tokens_total"] = fmt.Sprintf("%d", summary.Tokens.Total)
	}

	return DelegateResult{
		SessionID:   sess.ID,
		Observation: summary.Observation(),
		Done:        summary.Done(),
		Meta:        meta,
	}, nil
}

// Close stops the hook server and cleans up the hook config file.
func (a *CopilotAgent) Close(ctx context.Context) error {
	if a.hookServer != nil {
		return a.hookServer.Stop()
	}
	return nil
}

// --------------------------------------------------------------------------
// Tool registration (Motor layer bridge)
// --------------------------------------------------------------------------

// NewCopilotDelegateTool returns a ToolSpec that bridges the Motor layer to
// the CopilotAgent. The runtime is long-lived (singleton): the hook server
// is started once on first Invoke and reused across all calls.
func NewCopilotDelegateTool() ToolSpec {
	params := map[string]*schema.ParameterInfo{
		"task": {
			Type:     schema.String,
			Desc:     "Natural language description of what to delegate to GitHub Copilot CLI",
			Required: true,
		},
		"session_id": {
			Type: schema.String,
			Desc: "Copilot session ID to resume. Leave empty to start a new session (the assigned ID is returned in meta)",
		},
		"veto_level": {
			Type: schema.String,
			Desc: "Brainstem Veto security level: 'allow_all' (default), 'block_dangerous', or 'strict'",
		},
		"allow_all_tools": {
			Type: schema.Boolean,
			Desc: "Pass --allow-all-tools to Copilot CLI (equivalent to --yolo for tool permissions)",
		},
		"allow_tools": {
			Type: schema.String,
			Desc: "Comma-separated list of tool permission patterns to allow (e.g. 'shell(go:*),write')",
		},
		"deny_tools": {
			Type: schema.String,
			Desc: "Comma-separated list of tool permission patterns to deny",
		},
		"model": {
			Type: schema.String,
			Desc: "AI model to use (e.g. 'claude-sonnet-4.6'). Defaults to Copilot CLI's configured model",
		},
		"work_dir": {
			Type: schema.String,
			Desc: "Working directory for the Copilot subprocess. Defaults to current directory",
		},
	}

	return ToolSpec{
		Name:        "delegate.copilot",
		Kind:        ToolKindInternal,
		Description: "Delegate a coding or shell task to GitHub Copilot CLI. The Brainstem Veto intercepts all tool calls in real time via the hook system.",
		Params:      schema.NewParamsOneOfByParams(params),
		Load: func(ctx context.Context) (ToolRuntime, error) {
			vetoLevel := copilot.VetoLevelAllowAll // default: allow all
			veto := copilot.NewVetoEngine(vetoLevel)
			agent := NewCopilotAgent(veto)
			if err := agent.Start(ctx); err != nil {
				return nil, fmt.Errorf("copilot delegate tool: start agent: %w", err)
			}
			slog.Info("CopilotAgent started", "hook_addr", agent.hookServer.Addr())
			return &copilotDelegateRuntime{agent: agent}, nil
		},
	}
}

type copilotDelegateRuntime struct {
	agent *CopilotAgent
}

func (r *copilotDelegateRuntime) Invoke(ctx context.Context, command Command) (Result, error) {
	task, _ := command.Input["task"].(string)
	if strings.TrimSpace(task) == "" {
		return Result{}, fmt.Errorf("delegate.copilot: 'task' input is required")
	}

	dt := DelegateTask{
		Task:      task,
		SessionID: stringInput(command.Input, "session_id"),
		WorkDir:   stringInput(command.Input, "work_dir"),
		Meta: map[string]string{
			"model":           stringInput(command.Input, "model"),
			"allow_tools":     stringInput(command.Input, "allow_tools"),
			"deny_tools":      stringInput(command.Input, "deny_tools"),
			"allow_all_paths": "",
		},
	}

	// Veto level can be overridden per-invocation
	if lvlStr := stringInput(command.Input, "veto_level"); lvlStr != "" {
		r.agent.veto.Level = copilot.ParseVetoLevel(lvlStr)
	}

	if allowAll, ok := command.Input["allow_all_tools"].(bool); ok && allowAll {
		dt.Meta["allow_all_tools"] = "true"
	}

	res, err := r.agent.Delegate(ctx, dt)
	if err != nil {
		return Result{}, err
	}

	return Result{
		CommandID:   firstNonEmpty(command.ID, command.Action),
		Observation: res.Observation,
		Done:        res.Done,
		Meta:        res.Meta,
	}, nil
}

func (r *copilotDelegateRuntime) Close(ctx context.Context) error {
	return r.agent.Close(ctx)
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func stringInput(input map[string]interface{}, key string) string {
	if v, ok := input[key].(string); ok {
		return v
	}
	return ""
}
