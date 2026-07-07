package tests_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/brain"
	"github.com/qtopie/domour/internal/engine"
)

type mockExecutorClient struct{}

func (m *mockExecutorClient) Execute(ctx context.Context, command tool.Command) (tool.Result, error) {
	return tool.Result{
		CommandID:   command.ID,
		Observation: "mocked tool output",
		Done:        true,
	}, nil
}

func (m *mockExecutorClient) Veto(ctx context.Context, action string) bool {
	return false
}

func (m *mockExecutorClient) ListTools(ctx context.Context) ([]tool.ToolInfo, error) {
	return nil, nil
}

func (m *mockExecutorClient) ToolManager() *tool.Manager {
	return nil
}

func TestIntegrationReActCoordination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eng := engine.NewEngine(nil, &mockExecutorClient{})

	err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	sig := brain.SensorySignal{
		Source: "user",
		Data:   "Test react coordination integration",
	}

	err = eng.Submit(ctx, sig)
	if err != nil {
		t.Fatalf("failed to submit sensory signal: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Errorf("Timeout waiting for react integration test to complete")
	case resp := <-eng.Results():
		t.Logf("Received final react integration response: %+v", resp)
		if !resp.Success {
			t.Errorf("expected response success to be true, got false")
		}
		if !strings.Contains(resp.Output, "respond") {
			t.Errorf("expected response output to contain 'respond', got: %q", resp.Output)
		}
	}
}

func TestIntegrationSimpleModeOk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eng := engine.NewEngine(nil, &mockExecutorClient{})

	err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	sig := brain.SensorySignal{
		Source: "user",
		Data:   "Test simple mode integration",
	}

	err = eng.Submit(ctx, sig)
	if err != nil {
		t.Fatalf("failed to submit sensory signal: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Errorf("Timeout waiting for simple ok integration test to complete")
	case resp := <-eng.Results():
		t.Logf("Received final simple ok integration response: %+v", resp)
		if !resp.Success {
			t.Errorf("expected response success to be true, got false")
		}
		if !strings.Contains(resp.Output, "result: 4") {
			t.Errorf("expected response output to contain 'result: 4', got: %q", resp.Output)
		}
	}
}

func TestIntegrationSimpleModeCorrection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eng := engine.NewEngine(nil, &mockExecutorClient{})

	err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	sig := brain.SensorySignal{
		Source: "user",
		Data:   "Test simple mode with invalid 2+2 problem integration",
	}

	err = eng.Submit(ctx, sig)
	if err != nil {
		t.Fatalf("failed to submit sensory signal: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Errorf("Timeout waiting for simple correction integration test to complete")
	case resp := <-eng.Results():
		t.Logf("Received final simple correction integration response: %+v", resp)
		if !resp.Success {
			t.Errorf("expected response success to be true, got false")
		}
		if !strings.Contains(resp.Output, "corrected result: 2+2=4") {
			t.Errorf("expected response output to contain 'corrected result: 2+2=4', got: %q", resp.Output)
		}
	}
}
