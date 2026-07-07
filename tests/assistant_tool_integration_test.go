package tests_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/brain"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/engine"
	localorch "github.com/qtopie/domour/internal/infra/dapr/local"
	localbus "github.com/qtopie/domour/internal/infra/eventbus/local"
)

type mockAssistantDiencephalonClient struct {
	t          *testing.T
	step       int
	toolResult string
	boundTools []*schema.ToolInfo
}

func (m *mockAssistantDiencephalonClient) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	m.step++
	if m.step == 1 {
		// Verify tools were bound correctly
		if len(m.boundTools) == 0 {
			m.t.Errorf("expected bound tools, got none")
		}
		var foundGrep bool
		for _, t := range m.boundTools {
			if t.Name == "search_grep" {
				foundGrep = true
			}
		}
		if !foundGrep {
			m.t.Errorf("expected search_grep in bound tools")
		}

		// Propose search_grep tool call
		toolCall := schema.ToolCall{
			ID: "tc-grep-pattern",
			Function: schema.FunctionCall{
				Name:      "search_grep",
				Arguments: `{"pattern": "func main", "dir": "/src"}`,
			},
		}
		return &schema.Message{
			Role:      schema.Assistant,
			Content:   "I will use the tool to search.",
			ToolCalls: []schema.ToolCall{toolCall},
		}, nil
	} else if m.step == 2 {
		// Verify the observation was received
		var foundToolResult bool
		for _, msg := range messages {
			if msg.Role == schema.Tool && msg.ToolCallID == "tc-grep-pattern" {
				m.toolResult = msg.Content
				foundToolResult = true
			}
		}
		if !foundToolResult {
			m.t.Errorf("Expected tool result message in history but not found")
		}
		return &schema.Message{
			Role:    schema.Assistant,
			Content: fmt.Sprintf("Search result is: %s", m.toolResult),
		}, nil
	}
	return nil, fmt.Errorf("unexpected step: %d", m.step)
}

func (m *mockAssistantDiencephalonClient) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *mockAssistantDiencephalonClient) BindTools(tools []*schema.ToolInfo) error {
	m.boundTools = tools
	return nil
}

type mockAssistantCognitorClient struct {
	client *mockAssistantDiencephalonClient
}

func (m *mockAssistantCognitorClient) GetClient(ctx context.Context, entry string) (*proxy.Client, error) {
	return proxy.NewTestClient("mock-provider", "mock-model", m.client), nil
}

func (m *mockAssistantCognitorClient) GetClientWithOverride(ctx context.Context, entry string, provider, model string) (*proxy.Client, error) {
	return proxy.NewTestClient(provider, model, m.client), nil
}

type mockAssistantExecutorClient struct {
	manager *tool.Manager
}

func (m *mockAssistantExecutorClient) Execute(ctx context.Context, cmd tool.Command) (tool.Result, error) {
	return m.manager.Execute(ctx, cmd)
}

func (m *mockAssistantExecutorClient) Veto(ctx context.Context, action string) bool {
	return false
}

func (m *mockAssistantExecutorClient) ListTools(ctx context.Context) ([]tool.ToolInfo, error) {
	return m.manager.List(), nil
}

func (m *mockAssistantExecutorClient) ToolManager() *tool.Manager {
	return m.manager
}

type mockAssistantEngine struct {
	cognitor engine.CognitorClient
	executor engine.ExecutorClient
}

func (e *mockAssistantEngine) Cognitor() engine.CognitorClient { return e.cognitor }
func (e *mockAssistantEngine) Executor() engine.ExecutorClient { return e.executor }
func (e *mockAssistantEngine) Start(ctx context.Context) error { return nil }
func (e *mockAssistantEngine) Submit(ctx context.Context, signal brain.SensorySignal) error { return nil }
func (e *mockAssistantEngine) Results() <-chan brain.MotorFeedback { return nil }
func (e *mockAssistantEngine) AddObserver(sessionID string, obs engine.SignalObserver) {}
func (e *mockAssistantEngine) RemoveObserver(sessionID string) {}
func (e *mockAssistantEngine) Diencephalon() *brain.DiencephalonNode { return nil }

func TestAssistantServiceToolCallingLoop(t *testing.T) {
	ctx := context.Background()

	// 1. Setup mock LLM and tool manager
	mockLLM := &mockAssistantDiencephalonClient{t: t}
	cognitor := &mockAssistantCognitorClient{client: mockLLM}

	manager := tool.NewManager()
	grepTool := tool.NewInternalTool("search.grep", "Search files", func(ctx context.Context, cmd tool.Command) (tool.Result, error) {
		pattern, _ := cmd.Input["pattern"].(string)
		dir, _ := cmd.Input["dir"].(string)
		return tool.Result{
			CommandID:   cmd.ID,
			Observation: fmt.Sprintf("found pattern %q in %s", pattern, dir),
			Done:        true,
		}, nil
	})
	_ = manager.Register(grepTool)

	executor := &mockAssistantExecutorClient{manager: manager}
	eng := &mockAssistantEngine{cognitor: cognitor, executor: executor}

	// 2. Initialize AssistantService
	eb := localbus.NewEventBus()
	orch := localorch.NewLocalOrchestrator(eng, eb)
	service := assistant.NewAssistantService(eng, nil, eb, orch)

	// 3. Call Chat and verify streaming and results
	req := shared.MotorChatRequest{
		SessionID: "test-session",
		Seq:       1,
		Message:   "Search for func main",
	}

	var events []shared.MotorStreamEvent
	yield := func(ev shared.MotorStreamEvent) error {
		events = append(events, ev)
		return nil
	}

	err := service.Chat(ctx, req, "mock-provider", "mock-model", yield)
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}

	// 4. Verify step 2 output shows correct results
	var foundMotorCall bool
	var replyBuilder strings.Builder
	for _, ev := range events {
		if ev.Stage == "motor" && strings.Contains(ev.Content, "Calling tool \"search.grep\"") {
			foundMotorCall = true
		}
		if ev.Stage == "reply" {
			replyBuilder.WriteString(ev.Content)
		}
	}

	if !foundMotorCall {
		t.Errorf("expected motor call stream event, got none")
	}

	replyText := replyBuilder.String()
	expectedReply := "Search result is: found pattern \"func main\" in /src"
	if !strings.Contains(replyText, expectedReply) {
		t.Errorf("expected final reply containing %q, got %q", expectedReply, replyText)
	}
}
