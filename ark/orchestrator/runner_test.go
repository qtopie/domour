package orchestrator

import (
	"context"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/engine"
)

type mockDiencephalonClient struct{}

func (m *mockDiencephalonClient) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	return &schema.Message{
		Role:    schema.Assistant,
		Content: "mocked eino response",
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
	return proxy.NewTestClient("mock", "mock-model", m.client), nil
}

func (m *mockCognitorClient) GetClientWithOverride(ctx context.Context, entry string, provider, model string) (*proxy.Client, error) {
	return proxy.NewTestClient(provider, model, m.client), nil
}

func TestEinoNativeRunner_Run(t *testing.T) {
	cognitorClient := &mockCognitorClient{client: &mockDiencephalonClient{}}
	executorClient, err := engine.NewConfiguredExecutorClient()
	if err != nil {
		t.Fatalf("failed to init executor client: %v", err)
	}

	eng := engine.NewEngine(cognitorClient, executorClient)
	runner := NewEinoNativeRunner(eng)

	input := &AgentInput{
		SessionID: "test-session",
		Provider:  "mock",
		Model:     "mock-model",
		Messages: []*schema.Message{
			schema.UserMessage("Hello"),
		},
	}

	ctx := context.Background()
	msg, err := runner.Run(ctx, input)
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}
	if msg == nil || msg.Content != "mocked eino response" {
		t.Fatalf("expected 'mocked eino response', got %v", msg)
	}
}

func TestRunnerFactory(t *testing.T) {
	cognitorClient := &mockCognitorClient{client: &mockDiencephalonClient{}}
	executorClient, _ := engine.NewConfiguredExecutorClient()
	eng := engine.NewEngine(cognitorClient, executorClient)

	runner, err := NewRunner(Config{
		Mode:   ModeEinoNative,
		Engine: eng,
	})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	if runner == nil {
		t.Fatalf("expected non-nil runner")
	}
}
