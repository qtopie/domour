package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/brain"
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

func TestRuntimeBiomorphicPathways(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Instantiate the engine
	eng := NewEngine(nil, &mockExecutorClient{})

	// Start all loops
	err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	// Submit a semantic sensory signal
	sig := brain.SensorySignal{
		Source: "user",
		Data:   "Test biomorphic coordination loop",
	}

	err = eng.Submit(ctx, sig)
	if err != nil {
		t.Fatalf("failed to submit sensory signal: %v", err)
	}

	// Trace and verify the sequence of channel message deliveries.
	// The path should be:
	// 1. RawSensoryIn -> SensoryRelay -> SemanticOut -> Cerebrum.TaskIn
	// 2. Cerebrum.TaskIn -> Thinking loop -> ResultOut -> Diencephalon.CommandIn
	// 3. Diencephalon.CommandIn -> CommandOut -> Brainstem.CommandIn
	// 4. Brainstem.CommandIn -> Pons split -> PonsOut -> Cerebellum.CognitiveIn
	// 5. Cerebellum.CognitiveIn -> ExecutionOut -> Brainstem.ExecutionIn
	// 6. Brainstem.ExecutionIn -> FeedbackOut -> Cerebellum.FeedbackIn
	// 7. Cerebellum.FeedbackIn -> CorrectionOut -> Diencephalon.CorrectionIn
	// 8. Diencephalon.CorrectionIn -> CorrectionOut -> Cerebrum.CorrectionIn
	// 9. Cerebrum.CorrectionIn -> Thinking loop -> ResultOut (Plan: respond) -> Diencephalon.CommandIn
	// 10. Diencephalon.CommandIn -> CommandOut -> Brainstem.CommandIn
	// 11. Brainstem.CommandIn -> Pons split -> PonsOut -> Cerebellum.CognitiveIn
	// 12. Cerebellum.CognitiveIn -> ExecutionOut (ToolName: respond) -> Brainstem.ExecutionIn
	// 13. Brainstem.ExecutionIn -> ResponseOut -> Diencephalon.ResponseIn -> Diencephalon.ResponseOut (Results())
	select {
	case <-ctx.Done():
		t.Errorf("Timeout waiting for biomorphic loop to complete: channel routing deadlocked or failed")
	case resp := <-eng.Results():
		t.Logf("Received final biomorphic loop response: %+v", resp)
		if !resp.Success {
			t.Errorf("expected response success to be true, got false")
		}
		if !strings.Contains(resp.Output, "respond") {
			t.Errorf("expected response output to contain 'respond', got: %q", resp.Output)
		}
	}
}

func TestTimeoutAndRingBufferEviction(t *testing.T) {
	// 1. Test ring buffer evict and push functionality on channel
	ch := make(chan brain.SensorySignal, 2)
	
	brain.EvictAndPushChannel(ch, brain.SensorySignal{Source: "test1"})
	brain.EvictAndPushChannel(ch, brain.SensorySignal{Source: "test2"})
	
	// This push should evict "test1" and enqueue "test3"
	brain.EvictAndPushChannel(ch, brain.SensorySignal{Source: "test3"})
	
	if len(ch) != 2 {
		t.Fatalf("expected channel length 2, got %d", len(ch))
	}
	
	first := <-ch
	if first.Source != "test2" {
		t.Errorf("expected first element to be test2 (test1 evicted), got %s", first.Source)
	}
	
	second := <-ch
	if second.Source != "test3" {
		t.Errorf("expected second element to be test3, got %s", second.Source)
	}

	// 2. Test thread-safe memory ring buffer
	rb := brain.NewSensorySignalRingBuffer(3)
	rb.Push(brain.SensorySignal{Source: "m1"})
	rb.Push(brain.SensorySignal{Source: "m2"})
	rb.Push(brain.SensorySignal{Source: "m3"})
	rb.Push(brain.SensorySignal{Source: "m4"}) // Evicts m1

	if rb.Size() != 3 {
		t.Errorf("expected size 3, got %d", rb.Size())
	}

	val, ok := rb.Pop()
	if !ok || val.Source != "m2" {
		t.Errorf("expected m2 (m1 evicted), got %s", val.Source)
	}
}

func TestHierarchicalPlanExecute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eng := NewEngine(nil, &mockExecutorClient{})

	err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	sig := brain.SensorySignal{
		Source: "user",
		Data:   "Test nested plan execution",
	}

	err = eng.Submit(ctx, sig)
	if err != nil {
		t.Fatalf("failed to submit sensory signal: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Errorf("Timeout waiting for nested biomorphic loop to complete")
	case resp := <-eng.Results():
		t.Logf("Received final nested biomorphic loop response: %+v", resp)
		if !resp.Success {
			t.Errorf("expected response success to be true, got false")
		}
		if !strings.Contains(resp.Output, "respond") {
			t.Errorf("expected response output to contain 'respond', got: %q", resp.Output)
		}
		if !strings.Contains(resp.Output, "nested") {
			t.Errorf("expected response output to contain 'nested', got: %q", resp.Output)
		}
	}
}

func TestCerebrumReActCerebellumExecute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eng := NewEngine(nil, &mockExecutorClient{})

	err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	sig := brain.SensorySignal{
		Source: "user",
		Data:   "Test react coordination",
	}

	err = eng.Submit(ctx, sig)
	if err != nil {
		t.Fatalf("failed to submit sensory signal: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Errorf("Timeout waiting for react loop to complete")
	case resp := <-eng.Results():
		t.Logf("Received final react loop response: %+v", resp)
		if !resp.Success {
			t.Errorf("expected response success to be true, got false")
		}
		if !strings.Contains(resp.Output, "respond") {
			t.Errorf("expected response output to contain 'respond', got: %q", resp.Output)
		}
	}
}

func TestCerebrumSimpleCerebellumVerifyOk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eng := NewEngine(nil, &mockExecutorClient{})

	err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	sig := brain.SensorySignal{
		Source: "user",
		Data:   "Test simple mode",
	}

	err = eng.Submit(ctx, sig)
	if err != nil {
		t.Fatalf("failed to submit sensory signal: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Errorf("Timeout waiting for simple ok loop to complete")
	case resp := <-eng.Results():
		t.Logf("Received final simple ok loop response: %+v", resp)
		if !resp.Success {
			t.Errorf("expected response success to be true, got false")
		}
		if !strings.Contains(resp.Output, "result: 4") {
			t.Errorf("expected response output to contain 'result: 4', got: %q", resp.Output)
		}
	}
}

func TestCerebrumSimpleCerebellumVerifyProblemAndFix(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eng := NewEngine(nil, &mockExecutorClient{})

	err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	sig := brain.SensorySignal{
		Source: "user",
		Data:   "Test simple mode with invalid 2+2 problem",
	}

	err = eng.Submit(ctx, sig)
	if err != nil {
		t.Fatalf("failed to submit sensory signal: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Errorf("Timeout waiting for simple error correction loop to complete")
	case resp := <-eng.Results():
		t.Logf("Received final simple error correction response: %+v", resp)
		if !resp.Success {
			t.Errorf("expected response success to be true, got false")
		}
		if !strings.Contains(resp.Output, "corrected result: 2+2=4") {
			t.Errorf("expected response output to contain 'corrected result: 2+2=4', got: %q", resp.Output)
		}
	}
}


