package infra_test

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	infraspi "github.com/qtopie/domour/ark/spi/infra"
	"github.com/qtopie/domour/internal/engine"
	"github.com/qtopie/domour/internal/infra/eventbus/local"
)

// SPEC-SPI-001: Neutral WorkflowInput & WorkflowState contract mapping and serialization
func TestWorkflowInput_Serialization(t *testing.T) {
	input := infraspi.WorkflowInput{
		SessionID:   "sess-test-123",
		Messages:    []*schema.Message{schema.UserMessage("Hello world")},
		Provider:    "test-provider",
		Model:       "test-model",
		StreamFinal: true,
		Stage:       "chat",
		Reasoning:   "test-reasoning",
		Meta:        map[string]string{"foo": "bar"},
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal WorkflowInput: %v", err)
	}

	var decoded infraspi.WorkflowInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal WorkflowInput: %v", err)
	}

	if decoded.SessionID != input.SessionID {
		t.Errorf("expected session_id %s, got %s", input.SessionID, decoded.SessionID)
	}
	if len(decoded.Messages) != 1 || decoded.Messages[0].Content != "Hello world" {
		t.Errorf("expected message 'Hello world', got %+v", decoded.Messages)
	}
	if decoded.Meta["foo"] != "bar" {
		t.Errorf("expected meta foo=bar, got %+v", decoded.Meta)
	}

	state := infraspi.WorkflowState{
		Status: "completed",
		Result: schema.AssistantMessage("Response answer", nil),
	}
	stateData, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("failed to marshal WorkflowState: %v", err)
	}
	var decodedState infraspi.WorkflowState
	if err := json.Unmarshal(stateData, &decodedState); err != nil {
		t.Fatalf("failed to unmarshal WorkflowState: %v", err)
	}
	if decodedState.Status != "completed" || decodedState.Result.Content != "Response answer" {
		t.Errorf("unexpected decoded state: %+v", decodedState)
	}
}

// SPEC-SPI-002: Elimination of Dapr package dependency in internal/app/assistant
func TestNoDaprDependencyInAppAssistant(t *testing.T) {
	assistantDir := filepath.Join("..", "..", "..", "internal", "app", "assistant")
	fset := token.NewFileSet()

	entries, err := os.ReadDir(assistantDir)
	if err != nil {
		t.Fatalf("failed to read assistant dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(assistantDir, entry.Name())
		node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("failed to parse file %s: %v", entry.Name(), err)
		}

		for _, imp := range node.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(importPath, "dapr") {
				t.Errorf("file %s illegally imports dapr package: %s", entry.Name(), importPath)
			}
		}
	}
}

// SPEC-SPI-003: Core engine LocalOrchestrator compliance with neutral AgentOrchestrator interface
func TestLocalOrchestrator_NeutralInput(t *testing.T) {
	eb := local.NewEventBus()
	orch := engine.NewLocalOrchestrator(nil, nil, eb)

	// Validate interface compliance
	var _ infraspi.AgentOrchestrator = orch

	input := infraspi.WorkflowInput{
		SessionID: "sess-local-test",
		Messages:  []*schema.Message{schema.UserMessage("hi")},
		Provider:  "mock",
		Model:     "mock-model",
	}

	workflowID := "wf-test-neutral-1"
	id, err := orch.StartWorkflow(context.Background(), workflowID, input)
	if err != nil {
		t.Fatalf("failed to start workflow with neutral input: %v", err)
	}
	if id != workflowID {
		t.Errorf("expected workflow ID %s, got %s", workflowID, id)
	}

	status, err := orch.GetWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		t.Fatalf("failed to get workflow status: %v", err)
	}
	if status == nil {
		t.Fatalf("expected non-nil workflow status")
	}
}
