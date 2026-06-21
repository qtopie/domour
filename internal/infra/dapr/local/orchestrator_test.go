package local

import (
	"context"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
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

type mockEngine struct {
	cognitor *mockCognitorClient
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
