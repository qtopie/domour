package local

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/brain"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/engine"
	"github.com/qtopie/domour/internal/infra/dapr"
	"github.com/qtopie/domour/internal/infra/eventbus/local"
)

type mockDiencephalonClient struct{}

func (m *mockDiencephalonClient) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	return &schema.Message{
		Role:    schema.Assistant,
		Content: "Hello world reply",
	}, nil
}

func (m *mockDiencephalonClient) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, _ := m.Generate(ctx, messages, opts...)
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *mockDiencephalonClient) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

type mockCognitorClient struct {
	client *mockDiencephalonClient
}

func (m *mockCognitorClient) GetClient(ctx context.Context, entry string) (*proxy.Client, error) {
	return proxy.NewTestClient("mock-provider", "mock-model", m.client), nil
}

func (m *mockCognitorClient) GetClientWithOverride(ctx context.Context, entry string, provider, model string) (*proxy.Client, error) {
	return proxy.NewTestClient(provider, model, m.client), nil
}

type mockExecutorClient struct{}

func (m *mockExecutorClient) Execute(ctx context.Context, cmd tool.Command) (tool.Result, error) {
	return tool.Result{}, nil
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

type mockEngine struct {
	cognitor engine.CognitorClient
	executor *mockExecutorClient
}

func (m *mockEngine) Cognitor() engine.CognitorClient { return m.cognitor }
func (m *mockEngine) Executor() engine.ExecutorClient { return m.executor }
func (m *mockEngine) Start(ctx context.Context) error { return nil }
func (m *mockEngine) Submit(ctx context.Context, signal brain.SensorySignal) error { return nil }
func (m *mockEngine) Results() <-chan brain.MotorFeedback { return nil }
func (m *mockEngine) AddObserver(sessionID string, obs engine.SignalObserver) {}
func (m *mockEngine) RemoveObserver(sessionID string) {}
func (m *mockEngine) Diencephalon() *brain.DiencephalonNode { return nil }

func TestLocalOrchestrator_WorkflowLifecycle(t *testing.T) {
	eb := local.NewEventBus()
	defer eb.Close()

	cognitor := &mockCognitorClient{client: &mockDiencephalonClient{}}
	executor := &mockExecutorClient{}
	eng := &mockEngine{cognitor: cognitor, executor: executor}

	orch := NewLocalOrchestrator(eng, eb)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	workflowID := "test-workflow-123"
	input := dapr.AgentWorkflowInput{
		SessionID: "session-1",
		Messages: []*schema.Message{
			schema.UserMessage("Hi"),
		},
		Provider:    "mock-provider",
		Model:       "mock-model",
		StreamFinal: false,
		Stage:       "reply",
	}

	_, err := orch.StartWorkflow(ctx, workflowID, input)
	if err != nil {
		t.Fatalf("failed to start workflow: %v", err)
	}

	// Poll status until complete
	var state *dapr.WorkflowState
	for i := 0; i < 20; i++ {
		status, err := orch.GetWorkflowStatus(ctx, workflowID)
		if err != nil {
			t.Fatalf("failed to get status: %v", err)
		}
		if status.Status == "completed" {
			state = status
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if state == nil {
		t.Fatalf("workflow did not complete in time")
	}

	if state.Result == nil {
		t.Fatalf("expected non-nil result message")
	}

	expectedContent := "Hello world reply"
	if state.Result.Content != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, state.Result.Content)
	}
}

// chunkTypeTestDiencephalonClient simulates a DeepSeek-like model that streams
// reasoning_content chunks first, then content chunks.
type chunkTypeTestDiencephalonClient struct {
	t *testing.T
}

func (m *chunkTypeTestDiencephalonClient) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	return nil, nil
}

func (m *chunkTypeTestDiencephalonClient) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	// Simulate DeepSeek's streaming pattern:
	// 1. ReasoningContent chunks (mutually exclusive with Content)
	// 2. Content chunks
	chunks := []*schema.Message{
		{Role: schema.Assistant, ReasoningContent: "让我想想这个问题", Content: ""},
		{Role: schema.Assistant, ReasoningContent: "首先我需要理解用户需求", Content: ""},
		{Role: schema.Assistant, ReasoningContent: "最后综合分析给出答案", Content: ""},
		{Role: schema.Assistant, ReasoningContent: "", Content: "你好！"},
		{Role: schema.Assistant, ReasoningContent: "", Content: "我可以帮你解决这个问题。"},
	}
	return schema.StreamReaderFromArray(chunks), nil
}

func (m *chunkTypeTestDiencephalonClient) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

type chunkTypeTestCognitorClient struct {
	client *chunkTypeTestDiencephalonClient
}

func (m *chunkTypeTestCognitorClient) GetClient(ctx context.Context, entry string) (*proxy.Client, error) {
	return proxy.NewTestClient("mock-provider", "mock-model", m.client), nil
}

func (m *chunkTypeTestCognitorClient) GetClientWithOverride(ctx context.Context, entry string, provider, model string) (*proxy.Client, error) {
	return proxy.NewTestClient(provider, model, m.client), nil
}

func TestLocalOrchestrator_ChunkTypeClassification(t *testing.T) {
	eb := local.NewEventBus()
	defer eb.Close()

	mockLLM := &chunkTypeTestDiencephalonClient{t: t}
	cognitor := &chunkTypeTestCognitorClient{client: mockLLM}
	executor := &mockExecutorClient{}
	eng := &mockEngine{cognitor: cognitor, executor: executor}

	orch := NewLocalOrchestrator(eng, eb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	workflowID := "chunk-type-test-wf"
	topic := "agent/workflow/" + workflowID + "/stream"

	// Subscribe to event bus to capture streaming events
	var events []shared.MotorStreamEvent
	sub, err := eb.Subscribe(ctx, topic, func(data []byte) {
		var event shared.MotorStreamEvent
		if err := json.Unmarshal(data, &event); err != nil {
			t.Logf("Failed to unmarshal event: %v", err)
			return
		}
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("failed to subscribe to event bus: %v", err)
	}
	defer sub.Unsubscribe()

	input := dapr.AgentWorkflowInput{
		SessionID: "chunk-type-session",
		Messages: []*schema.Message{
			schema.UserMessage("帮我查一下设计文档"),
		},
		Provider:    "mock-provider",
		Model:       "mock-model",
		StreamFinal: true,
		Stage:       "reply",
	}

	_, err = orch.StartWorkflow(ctx, workflowID, input)
	if err != nil {
		t.Fatalf("failed to start workflow: %v", err)
	}

	// Wait for workflow to complete
	var state *dapr.WorkflowState
	for i := 0; i < 50; i++ {
		status, err := orch.GetWorkflowStatus(ctx, workflowID)
		if err != nil {
			t.Fatalf("failed to get workflow status: %v", err)
		}
		if status.Status == "completed" {
			state = status
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if state == nil {
		t.Fatalf("workflow did not complete in time")
	}

	// Allow a small delay for event bus to flush
	time.Sleep(50 * time.Millisecond)

	// Analyze captured events
	var thinkingChunks, textChunks int
	var hasDoneEvent bool
	var lastDoneEventErr bool

	for _, ev := range events {
		switch ev.Type {
		case 1: // CHUNK_THINKING
			thinkingChunks++
			t.Logf("[THINKING] Content: %q", ev.Content)
			if ev.Thinking == nil {
				t.Logf("WARNING: type 1 event without Thinking detail")
			}
		case 0: // CHUNK_TEXT
			textChunks++
			t.Logf("[TEXT] Content: %q", ev.Content)
		}
		if ev.Done {
			hasDoneEvent = true
			lastDoneEventErr = ev.Err != nil
		}
	}

	// Verify streaming yields
	if thinkingChunks == 0 {
		t.Error("Expected at least one CHUNK_THINKING (type 2) event for ReasoningContent chunks, got 0")
	}
	if textChunks == 0 {
		t.Error("Expected at least one CHUNK_TEXT (type 1) event for Content chunks, got 0")
	}
	if !hasDoneEvent {
		t.Error("Expected a Done event at the end of the stream")
	}
	if lastDoneEventErr {
		t.Error("Done event should not have an error")
	}

	// Verify final result content
	if state.Result == nil {
		t.Fatal("expected non-nil result message")
	}
	expectedContent := "你好！我可以帮你解决这个问题。"
	if state.Result.Content != expectedContent {
		t.Errorf("expected result content %q, got %q", expectedContent, state.Result.Content)
	}
	if state.Result.ReasoningContent != "让我想想这个问题首先我需要理解用户需求最后综合分析给出答案" {
		t.Errorf("expected reasoning content to be concatenated, got %q", state.Result.ReasoningContent)
	}
}
