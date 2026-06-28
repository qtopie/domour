package copilot

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// VetoLevel controls how strictly the Brainstem Veto engine evaluates tool calls
// intercepted from Copilot CLI via the preToolUse hook.
type VetoLevel int

const (
	// VetoLevelAllowAll permits every tool call. Useful for development and trusted environments.
	VetoLevelAllowAll VetoLevel = iota
	// VetoLevelBlockDangerous blocks a hardcoded set of obviously destructive shell commands.
	VetoLevelBlockDangerous
	// VetoLevelStrict only permits tools and commands that are explicitly in the AllowList.
	VetoLevelStrict
)

// ParseVetoLevel converts a string name to a VetoLevel.
func ParseVetoLevel(s string) VetoLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "block_dangerous", "block-dangerous":
		return VetoLevelBlockDangerous
	case "strict":
		return VetoLevelStrict
	default:
		return VetoLevelAllowAll
	}
}

func (v VetoLevel) String() string {
	switch v {
	case VetoLevelBlockDangerous:
		return "block_dangerous"
	case VetoLevelStrict:
		return "strict"
	default:
		return "allow_all"
	}
}

// HookPayload is the JSON object Copilot CLI sends via stdin (or HTTP body)
// for preToolUse and postToolUse hooks.
type HookPayload struct {
	// camelCase format (native Copilot CLI)
	SessionID string `json:"sessionId"`
	Timestamp int64  `json:"timestamp"`
	Cwd       string `json:"cwd"`
	ToolName  string `json:"toolName"`
	ToolArgs  any    `json:"toolArgs"`

	// snake_case format (VS Code / Claude-compatible)
	HookEventName string `json:"hook_event_name"`
	ToolNameSnake string `json:"tool_name"`
	ToolInputSnake any    `json:"tool_input"`
}

// EffectiveToolName returns the tool name regardless of which field format was used.
func (p HookPayload) EffectiveToolName() string {
	if p.ToolName != "" {
		return p.ToolName
	}
	return p.ToolNameSnake
}

// EffectiveToolArgs returns tool arguments regardless of field format.
func (p HookPayload) EffectiveToolArgs() any {
	if p.ToolArgs != nil {
		return p.ToolArgs
	}
	return p.ToolInputSnake
}

// VetoDecision is the JSON Domour writes back to Copilot's preToolUse hook.
type VetoDecision struct {
	// PermissionDecision is "allow", "deny", or "ask".
	PermissionDecision string `json:"permissionDecision"`
	// PermissionDecisionReason is required when denying.
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	// ModifiedArgs lets the veto engine substitute safer arguments.
	ModifiedArgs any `json:"modifiedArgs,omitempty"`
}

// VetoEngine evaluates preToolUse hook payloads and returns decisions.
type VetoEngine struct {
	Level     VetoLevel
	AllowList []string // tool names allowed in VetoLevelStrict mode
	DenyList  []string // additional commands to block (augments built-in rules)
}

// NewVetoEngine creates a VetoEngine with the given security level.
func NewVetoEngine(level VetoLevel) *VetoEngine {
	return &VetoEngine{Level: level}
}

// Evaluate decides whether to allow, deny, or ask for a tool invocation.
func (e *VetoEngine) Evaluate(payload HookPayload) VetoDecision {
	toolName := payload.EffectiveToolName()
	toolArgs := payload.EffectiveToolArgs()

	slog.Debug("Veto engine evaluating tool call",
		"tool", toolName,
		"level", e.Level.String(),
	)

	switch e.Level {
	case VetoLevelAllowAll:
		return VetoDecision{PermissionDecision: "allow"}

	case VetoLevelBlockDangerous:
		if reason, blocked := e.checkDangerousCommand(toolName, toolArgs); blocked {
			slog.Warn("Veto DENY: dangerous command intercepted", "tool", toolName, "reason", reason)
			return VetoDecision{
				PermissionDecision:       "deny",
				PermissionDecisionReason: reason,
			}
		}
		// Also check caller-specified deny list
		if reason, blocked := e.checkDenyList(toolName, toolArgs); blocked {
			slog.Warn("Veto DENY: command on deny list", "tool", toolName, "reason", reason)
			return VetoDecision{
				PermissionDecision:       "deny",
				PermissionDecisionReason: reason,
			}
		}
		return VetoDecision{PermissionDecision: "allow"}

	case VetoLevelStrict:
		if !e.inAllowList(toolName) {
			reason := fmt.Sprintf("tool %q is not in the Domour allow list", toolName)
			slog.Warn("Veto DENY: tool not in allow list", "tool", toolName)
			return VetoDecision{
				PermissionDecision:       "deny",
				PermissionDecisionReason: reason,
			}
		}
		// Even in strict mode, dangerous built-in blocks apply inside allowed tools
		if reason, blocked := e.checkDangerousCommand(toolName, toolArgs); blocked {
			return VetoDecision{
				PermissionDecision:       "deny",
				PermissionDecisionReason: reason,
			}
		}
		return VetoDecision{PermissionDecision: "allow"}
	}

	return VetoDecision{PermissionDecision: "allow"}
}

// EvaluateJSON parses a raw JSON hook payload and returns the decision JSON bytes.
func (e *VetoEngine) EvaluateJSON(raw []byte) ([]byte, error) {
	var payload HookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		// If we can't parse the payload at all, allow by default (fail-open)
		slog.Warn("Veto engine could not parse hook payload, defaulting to allow", "error", err)
		return json.Marshal(VetoDecision{PermissionDecision: "allow"})
	}
	decision := e.Evaluate(payload)
	return json.Marshal(decision)
}

// dangerousPatterns are shell command fragments that indicate destructive intent.
var dangerousPatterns = []string{
	"rm -rf /",
	"rm -rf /*",
	"rm -rf ~",
	"rm -fr /",
	":(){ :|:& };:", // fork bomb
	"mkfs",
	"dd if=",
	"> /dev/sda",
	"> /dev/disk",
	"shred /dev",
	"chmod -R 777 /",
	"chmod 000 /",
	"chown -R root /",
}

func (e *VetoEngine) checkDangerousCommand(toolName string, toolArgs any) (string, bool) {
	if toolName != "bash" && toolName != "shell" && toolName != "powershell" {
		return "", false
	}
	cmd := extractCommandString(toolArgs)
	if cmd == "" {
		return "", false
	}
	lower := strings.ToLower(cmd)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lower, pattern) {
			return fmt.Sprintf("command matches dangerous pattern %q", pattern), true
		}
	}
	return "", false
}

func (e *VetoEngine) checkDenyList(toolName string, toolArgs any) (string, bool) {
	cmd := strings.ToLower(extractCommandString(toolArgs))
	for _, denied := range e.DenyList {
		if strings.Contains(cmd, strings.ToLower(denied)) {
			return fmt.Sprintf("command matches deny list entry %q", denied), true
		}
	}
	_ = toolName
	return "", false
}

func (e *VetoEngine) inAllowList(toolName string) bool {
	for _, allowed := range e.AllowList {
		if strings.EqualFold(allowed, toolName) {
			return true
		}
	}
	return false
}

// extractCommandString pulls the shell command string from toolArgs regardless of structure.
func extractCommandString(toolArgs any) string {
	if toolArgs == nil {
		return ""
	}
	switch v := toolArgs.(type) {
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"command", "cmd", "input", "bash"} {
			if val, ok := v[key]; ok {
				if s, ok := val.(string); ok {
					return s
				}
			}
		}
	}
	// Try JSON encode and look for "command" key
	b, _ := json.Marshal(toolArgs)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err == nil {
		for _, key := range []string{"command", "cmd", "input", "bash"} {
			if val, ok := m[key]; ok {
				if s, ok := val.(string); ok {
					return s
				}
			}
		}
	}
	return ""
}
