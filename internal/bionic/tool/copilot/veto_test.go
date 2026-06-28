package copilot

import (
	"encoding/json"
	"strings"
	"testing"
)

// --------------------------------------------------------------------------
// VetoEngine tests
// --------------------------------------------------------------------------

func TestVetoLevel_AllowAll(t *testing.T) {
	engine := NewVetoEngine(VetoLevelAllowAll)

	cases := []struct {
		toolName string
		args     any
	}{
		{"bash", map[string]any{"command": "rm -rf /"}},
		{"bash", map[string]any{"command": ":(){ :|:& };:"}},
		{"edit", map[string]any{"path": "/etc/passwd"}},
		{"create", nil},
	}

	for _, tc := range cases {
		d := engine.Evaluate(HookPayload{ToolName: tc.toolName, ToolArgs: tc.args})
		if d.PermissionDecision != "allow" {
			t.Errorf("AllowAll: expected allow for tool=%s, got %s (%s)",
				tc.toolName, d.PermissionDecision, d.PermissionDecisionReason)
		}
	}
}

func TestVetoLevel_BlockDangerous(t *testing.T) {
	engine := NewVetoEngine(VetoLevelBlockDangerous)

	denied := []struct {
		toolName string
		cmd      string
	}{
		{"bash", "rm -rf /"},
		{"bash", "rm -rf /home"},
		{"bash", ":(){ :|:& };:"},
		{"bash", "dd if=/dev/zero of=/dev/sda"},
		{"bash", "mkfs.ext4 /dev/sda1"},
	}

	for _, tc := range denied {
		d := engine.Evaluate(HookPayload{
			ToolName: tc.toolName,
			ToolArgs: map[string]any{"command": tc.cmd},
		})
		if d.PermissionDecision != "deny" {
			t.Errorf("BlockDangerous: expected deny for cmd=%q, got %s", tc.cmd, d.PermissionDecision)
		}
	}

	allowed := []struct {
		toolName string
		cmd      string
	}{
		{"bash", "go test ./..."},
		{"bash", "ls -la"},
		{"bash", "git status"},
		{"edit", ""},
		{"view", ""},
	}

	for _, tc := range allowed {
		d := engine.Evaluate(HookPayload{
			ToolName: tc.toolName,
			ToolArgs: map[string]any{"command": tc.cmd},
		})
		if d.PermissionDecision != "allow" {
			t.Errorf("BlockDangerous: expected allow for tool=%s cmd=%q, got %s (%s)",
				tc.toolName, tc.cmd, d.PermissionDecision, d.PermissionDecisionReason)
		}
	}
}

func TestVetoLevel_Strict(t *testing.T) {
	engine := NewVetoEngine(VetoLevelStrict)
	engine.AllowList = []string{"bash", "view", "edit"}

	// Allowed tool, safe command
	d := engine.Evaluate(HookPayload{
		ToolName: "bash",
		ToolArgs: map[string]any{"command": "go build ./..."},
	})
	if d.PermissionDecision != "allow" {
		t.Errorf("Strict: expected allow for bash/go build, got %s", d.PermissionDecision)
	}

	// Allowed tool, dangerous command — still blocked
	d = engine.Evaluate(HookPayload{
		ToolName: "bash",
		ToolArgs: map[string]any{"command": "rm -rf /"},
	})
	if d.PermissionDecision != "deny" {
		t.Errorf("Strict: expected deny for bash/rm -rf /, got %s", d.PermissionDecision)
	}

	// Tool not in allow list
	d = engine.Evaluate(HookPayload{ToolName: "create", ToolArgs: nil})
	if d.PermissionDecision != "deny" {
		t.Errorf("Strict: expected deny for create (not in allowlist), got %s", d.PermissionDecision)
	}
}

func TestVetoEngine_DenyList(t *testing.T) {
	engine := NewVetoEngine(VetoLevelBlockDangerous)
	engine.DenyList = []string{"git push --force"}

	d := engine.Evaluate(HookPayload{
		ToolName: "bash",
		ToolArgs: map[string]any{"command": "git push --force origin main"},
	})
	if d.PermissionDecision != "deny" {
		t.Errorf("expected deny for custom deny list entry, got %s", d.PermissionDecision)
	}
}

func TestVetoEngine_EvaluateJSON(t *testing.T) {
	engine := NewVetoEngine(VetoLevelBlockDangerous)

	payload := HookPayload{
		SessionID: "test-session",
		ToolName:  "bash",
		ToolArgs:  map[string]any{"command": "go test ./..."},
	}
	raw, _ := json.Marshal(payload)

	out, err := engine.EvaluateJSON(raw)
	if err != nil {
		t.Fatalf("EvaluateJSON error: %v", err)
	}

	var decision VetoDecision
	if err := json.Unmarshal(out, &decision); err != nil {
		t.Fatalf("unmarshal decision: %v", err)
	}
	if decision.PermissionDecision != "allow" {
		t.Errorf("expected allow, got %s", decision.PermissionDecision)
	}
}

func TestVetoEngine_EvaluateJSON_InvalidPayload(t *testing.T) {
	engine := NewVetoEngine(VetoLevelBlockDangerous)
	// Invalid JSON → should fail-open (allow)
	out, err := engine.EvaluateJSON([]byte("{not valid json}"))
	if err != nil {
		t.Fatalf("EvaluateJSON should not error on bad payload: %v", err)
	}
	var d VetoDecision
	_ = json.Unmarshal(out, &d)
	if d.PermissionDecision != "allow" {
		t.Errorf("expected fail-open (allow) for bad payload, got %s", d.PermissionDecision)
	}
}

func TestParseVetoLevel(t *testing.T) {
	cases := map[string]VetoLevel{
		"allow_all":       VetoLevelAllowAll,
		"block_dangerous": VetoLevelBlockDangerous,
		"block-dangerous": VetoLevelBlockDangerous,
		"strict":          VetoLevelStrict,
		"STRICT":          VetoLevelStrict,
		"":                VetoLevelAllowAll,
		"unknown":         VetoLevelAllowAll,
	}
	for input, expected := range cases {
		got := ParseVetoLevel(input)
		if got != expected {
			t.Errorf("ParseVetoLevel(%q) = %v, want %v", input, got, expected)
		}
	}
}

func TestHookPayload_EffectiveMethods(t *testing.T) {
	// camelCase format
	p1 := HookPayload{ToolName: "bash", ToolArgs: map[string]any{"command": "ls"}}
	if p1.EffectiveToolName() != "bash" {
		t.Errorf("expected bash, got %s", p1.EffectiveToolName())
	}

	// snake_case format (VS Code compat)
	p2 := HookPayload{ToolNameSnake: "edit", ToolInputSnake: map[string]any{"path": "/tmp/x"}}
	if p2.EffectiveToolName() != "edit" {
		t.Errorf("expected edit, got %s", p2.EffectiveToolName())
	}
}

// --------------------------------------------------------------------------
// Stream parser tests
// --------------------------------------------------------------------------

func TestStreamParser_Collect(t *testing.T) {
	jsonl := strings.Join([]string{
		`{"type":"assistant","content":"I will list the files."}`,
		`{"type":"tool","name":"bash","input":{"command":"ls"}}`,
		`{"type":"tool_result","tool":"bash","output":"main.go\ngo.mod"}`,
		`{"type":"stats","tokens":{"prompt":100,"completion":50,"total":150}}`,
	}, "\n")

	parser := NewStreamParser(strings.NewReader(jsonl))
	summary, err := parser.Collect()
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if summary.AssistantText != "I will list the files." {
		t.Errorf("unexpected assistant text: %q", summary.AssistantText)
	}
	if len(summary.ToolOutputs) != 1 {
		t.Errorf("expected 1 tool output, got %d", len(summary.ToolOutputs))
	}
	if summary.Tokens == nil || summary.Tokens.Total != 150 {
		t.Errorf("expected token total=150, got %+v", summary.Tokens)
	}
	if !summary.Done() {
		t.Error("expected Done() to be true")
	}
}

func TestStreamParser_ToolError(t *testing.T) {
	jsonl := `{"type":"tool_result","tool":"bash","error":"command not found: foobar"}`
	parser := NewStreamParser(strings.NewReader(jsonl))
	summary, _ := parser.Collect()
	if len(summary.ToolErrors) == 0 {
		t.Error("expected tool error to be recorded")
	}
}

func TestStreamParser_NonJSONLine(t *testing.T) {
	// Non-JSON lines should be emitted as info events, not cause errors.
	jsonl := "GitHub Copilot CLI v1.0\n" +
		`{"type":"assistant","content":"hello"}`
	parser := NewStreamParser(strings.NewReader(jsonl))
	summary, err := parser.Collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.AssistantText != "hello" {
		t.Errorf("expected 'hello', got %q", summary.AssistantText)
	}
}

func TestStreamSummary_Observation(t *testing.T) {
	s := &StreamSummary{
		AssistantText: "Done.",
		ToolOutputs:   []string{"[bash]\nok"},
	}
	obs := s.Observation()
	if !strings.Contains(obs, "Done.") || !strings.Contains(obs, "[bash]") {
		t.Errorf("unexpected observation: %q", obs)
	}
}
