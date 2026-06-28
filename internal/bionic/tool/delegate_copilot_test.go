package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qtopie/domour/internal/bionic/tool/copilot"
)

// --------------------------------------------------------------------------
// Unit tests — no actual Copilot binary required
// --------------------------------------------------------------------------

func TestNewCopilotDelegateTool_Spec(t *testing.T) {
	spec := NewCopilotDelegateTool()
	if spec.Name != "delegate.copilot" {
		t.Errorf("unexpected tool name: %s", spec.Name)
	}
	if spec.Kind != ToolKindInternal {
		t.Errorf("unexpected kind: %s", spec.Kind)
	}
	if spec.Load == nil {
		t.Error("Load function must not be nil")
	}
}

func TestCopilotAgent_Name(t *testing.T) {
	veto := copilot.NewVetoEngine(copilot.VetoLevelAllowAll)
	agent := NewCopilotAgent(veto)
	if agent.Name() != "copilot" {
		t.Errorf("expected 'copilot', got %q", agent.Name())
	}
}

func TestCopilotAgent_RequiresStart(t *testing.T) {
	veto := copilot.NewVetoEngine(copilot.VetoLevelAllowAll)
	agent := NewCopilotAgent(veto)
	// Delegate without Start should return an error
	_, err := agent.Delegate(context.Background(), DelegateTask{Task: "hello"})
	if err == nil {
		t.Error("expected error when Start() not called")
	}
}

func TestCopilotDelegateRuntime_MissingTask(t *testing.T) {
	// Create an agent that has been started (hookServer not nil) by using a real Start()
	// call against a temporary hook dir, or just test the input validation path via Invoke
	// with a stub. We test the nil-check separately in TestCopilotAgent_RequiresStart.
	// Here we only verify that a missing task returns the correct error.
	veto := copilot.NewVetoEngine(copilot.VetoLevelAllowAll)
	agent := NewCopilotAgent(veto)
	// Start to initialise hook server (binds a random port, writes hook config)
	if err := agent.Start(context.Background()); err != nil {
		t.Skipf("cannot start agent in this environment: %v", err)
	}
	defer agent.Close(context.Background())

	rt := &copilotDelegateRuntime{agent: agent}
	_, err := rt.Invoke(context.Background(), Command{
		Action: "delegate.copilot",
		Input:  map[string]interface{}{},
	})
	if err == nil || !strings.Contains(err.Error(), "'task' input is required") {
		t.Errorf("expected task-required error, got: %v", err)
	}
}

func TestSplitComma(t *testing.T) {
	cases := map[string][]string{
		"shell(go:*),write":      {"shell(go:*)", "write"},
		"  a , b , c  ":         {"a", "b", "c"},
		"":                       nil,
		"single":                 {"single"},
	}
	for input, expected := range cases {
		got := splitComma(input)
		if len(got) != len(expected) {
			t.Errorf("splitComma(%q): got %v, want %v", input, got, expected)
			continue
		}
		for i := range got {
			if got[i] != expected[i] {
				t.Errorf("splitComma(%q)[%d]: got %q, want %q", input, i, got[i], expected[i])
			}
		}
	}
}

// --------------------------------------------------------------------------
// Hook Server integration test (no Copilot binary needed)
// --------------------------------------------------------------------------

func TestHookServer_PreToolUse_AllowAll(t *testing.T) {
	veto := copilot.NewVetoEngine(copilot.VetoLevelAllowAll)
	server := copilot.NewHookServer(veto)

	// Use httptest to call the handler directly without binding a port
	payload := copilot.HookPayload{
		SessionID: "test-session",
		ToolName:  "bash",
		ToolArgs:  map[string]any{"command": "rm -rf /"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/hooks/preToolUse", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	// Access the exported handler via the test helper shim
	server.ServePreToolUse(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var decision copilot.VetoDecision
	if err := json.NewDecoder(rr.Body).Decode(&decision); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	// AllowAll should allow even rm -rf /
	if decision.PermissionDecision != "allow" {
		t.Errorf("expected allow, got %s", decision.PermissionDecision)
	}
}

func TestHookServer_PreToolUse_BlockDangerous(t *testing.T) {
	veto := copilot.NewVetoEngine(copilot.VetoLevelBlockDangerous)
	server := copilot.NewHookServer(veto)

	payload := copilot.HookPayload{
		SessionID: "test-session",
		ToolName:  "bash",
		ToolArgs:  map[string]any{"command": "rm -rf /"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/hooks/preToolUse", strings.NewReader(string(body)))
	rr := httptest.NewRecorder()
	server.ServePreToolUse(rr, req)

	var decision copilot.VetoDecision
	_ = json.NewDecoder(rr.Body).Decode(&decision)
	if decision.PermissionDecision != "deny" {
		t.Errorf("expected deny for rm -rf /, got %s", decision.PermissionDecision)
	}
}
