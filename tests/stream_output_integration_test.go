package tests_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/brain"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/engine"
)

// streamTestDiencephalonClient mocks the LLM streaming responses to test the tag parser.
type streamTestDiencephalonClient struct {
	t          *testing.T
	step       int
	boundTools []*schema.ToolInfo
}

func (m *streamTestDiencephalonClient) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	return nil, nil
}

func (m *streamTestDiencephalonClient) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	m.step++
	if m.step == 1 {
		// Yield chunks with mixed think tags and a tool call proposal
		toolCall := schema.ToolCall{
			ID: "tc-stream-test",
			Function: schema.FunctionCall{
				Name:      "test_search",
				Arguments: `{"query": "domour design"}`,
			},
		}

		chunks := []*schema.Message{
			{Role: schema.Assistant, Content: "Hello. <think>Let me "},
			{Role: schema.Assistant, Content: "check the design docs."},
			{Role: schema.Assistant, Content: "</think>\nCalling search..."},
			{Role: schema.Assistant, ToolCalls: []schema.ToolCall{toolCall}},
		}
		return schema.StreamReaderFromArray(chunks), nil
	} else if m.step == 2 {
		// Yield final answer chunk with a think tag
		chunks := []*schema.Message{
			{Role: schema.Assistant, Content: "Based on search, <think>design matches.</think> Domour is modular."},
		}
		return schema.StreamReaderFromArray(chunks), nil
	}
	return nil, fmt.Errorf("unexpected step: %d", m.step)
}

func (m *streamTestDiencephalonClient) BindTools(tools []*schema.ToolInfo) error {
	m.boundTools = tools
	return nil
}

type streamTestCognitorClient struct {
	client *streamTestDiencephalonClient
}

func (m *streamTestCognitorClient) GetClient(ctx context.Context, entry string) (*proxy.Client, error) {
	return proxy.NewTestClient("mock-provider", "mock-model", m.client), nil
}

func (m *streamTestCognitorClient) GetClientWithOverride(ctx context.Context, entry string, provider, model string) (*proxy.Client, error) {
	return proxy.NewTestClient(provider, model, m.client), nil
}

type streamTestExecutorClient struct {
	manager *tool.Manager
}

func (m *streamTestExecutorClient) Execute(ctx context.Context, cmd tool.Command) (tool.Result, error) {
	return m.manager.Execute(ctx, cmd)
}

func (m *streamTestExecutorClient) Veto(ctx context.Context, action string) bool {
	return false
}

func (m *streamTestExecutorClient) ListTools(ctx context.Context) ([]tool.ToolInfo, error) {
	return m.manager.List(), nil
}

func (m *streamTestExecutorClient) ToolManager() *tool.Manager {
	return m.manager
}
func TestStreamOutputReActTagAndToolClassification(t *testing.T) {
	ctx := context.Background()

	// 1. Setup mock LLM and tool manager
	mockLLM := &streamTestDiencephalonClient{t: t}
	cognitor := &streamTestCognitorClient{client: mockLLM}

	manager := tool.NewManager()
	searchTool := tool.NewInternalTool("test_search", "Search files", func(ctx context.Context, cmd tool.Command) (tool.Result, error) {
		query, _ := cmd.Input["query"].(string)
		return tool.Result{
			CommandID:   cmd.ID,
			Observation: fmt.Sprintf("found match for %q", query),
			Done:        true,
		}, nil
	})
	_ = manager.Register(searchTool)

	executor := &streamTestExecutorClient{manager: manager}
	eng := engine.NewEngine(cognitor, executor)

	// 2. Initialize AssistantService
	service := assistant.NewAssistantService(eng, nil)

	// Create and persist a session history
	sess, err := service.GetSession(ctx, "stream-test-session")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.History = []shared.Message{}

	// 3. Call Chat and verify streaming classifications
	req := shared.MotorChatRequest{
		SessionID: "stream-test-session",
		Seq:       1,
		Message:   "Check domour design docs",
	}

	var events []shared.MotorStreamEvent
	yield := func(ev shared.MotorStreamEvent) error {
		events = append(events, ev)
		return nil
	}

	err = service.Chat(ctx, req, "mock-provider", "mock-model", yield)
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}

	// 4. Verify stream classification output
	var hasTextChunk, hasThinkingChunk, hasToolStartChunk, hasToolEndChunk bool

	for _, ev := range events {
		switch ev.Type {
		case 0: // CHUNK_TEXT
			hasTextChunk = true
			t.Logf("[Text] Content: %q", ev.Content)
		case 1: // CHUNK_THINKING
			hasThinkingChunk = true
			if ev.Thinking != nil {
				t.Logf("[Thinking Start] Engine: %s, Stage: %s", ev.Thinking.Engine, ev.Thinking.Stage)
			} else {
				t.Logf("[Thinking Content] Content: %q", ev.Content)
			}
		case 2: // CHUNK_TOOL_CALL
			if ev.ToolCall == nil {
				t.Errorf("Expected ToolCall detail for CHUNK_TOOL_CALL event")
			} else {
				if ev.ToolCall.Status == "started" {
					hasToolStartChunk = true
					t.Logf("[Tool Start] Tool: %q, Args: %s", ev.ToolCall.ToolName, ev.ToolCall.Arguments)
				} else if ev.ToolCall.Status == "completed" {
					hasToolEndChunk = true
					t.Logf("[Tool End] Tool: %q, Observation: %q", ev.ToolCall.ToolName, ev.ToolCall.Observation)
				}
			}
		}
	}

	if !hasTextChunk {
		t.Error("Expected at least one CHUNK_TEXT event")
	}
	if !hasThinkingChunk {
		t.Error("Expected at least one CHUNK_THINKING event from <think> parsing")
	}
	if !hasToolStartChunk {
		t.Error("Expected tool start event")
	}
	if !hasToolEndChunk {
		t.Error("Expected tool completed event")
	}
}

func TestStreamOutputCollaborationSignalInterception(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 1. Create a coreEngine with mock executor
	mockExec := &streamTestExecutorClient{}
	eng := engine.NewEngine(nil, mockExec)

	err := eng.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start coreEngine: %v", err)
	}

	sessionID := "stream-collaboration-test-session"
	sigEvents := make(chan engine.SignalEvent, 10)

	// 2. Add observer for the specific SessionID
	eng.AddObserver(sessionID, func(ev engine.SignalEvent) {
		sigEvents <- ev
	})
	defer eng.RemoveObserver(sessionID)

	// 3. Submit SensorySignal carrying the correlated SessionID
	sig := brain.SensorySignal{
		Ctx:       ctx,
		SessionID: sessionID,
		Source:    "user",
		Data:      "Test simple mode integration",
	}

	err = eng.Submit(ctx, sig)
	if err != nil {
		t.Fatalf("failed to submit sensory signal: %v", err)
	}

	// 4. Wait for the engine execution to finish and verify signal events
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for engine result")
	case res := <-eng.Results():
		t.Logf("Final engine result received: %+v", res)
	}

	// Read and verify the collected signal events
	close(sigEvents)
	var hasSensoryRelay, hasCognitivePlan, hasCommandRelay bool

	for ev := range sigEvents {
		if ev.SessionID != sessionID {
			t.Errorf("Expected SessionID %q, got: %q", sessionID, ev.SessionID)
		}

		switch ev.EventType {
		case "sensory_relay":
			hasSensoryRelay = true
			t.Logf("[Collaboration Signal] From: %s, To: %s, EventType: %s, Desc: %q", ev.FromNode, ev.ToNode, ev.EventType, ev.Description)
			if ev.FromNode != "diencephalon" || ev.ToNode != "cerebrum" {
				t.Errorf("Unexpected sensory_relay node direction: %s -> %s", ev.FromNode, ev.ToNode)
			}
		case "cognitive_plan":
			hasCognitivePlan = true
			t.Logf("[Collaboration Signal] From: %s, To: %s, EventType: %s, Desc: %q", ev.FromNode, ev.ToNode, ev.EventType, ev.Description)
			if ev.FromNode != "cerebrum" || ev.ToNode != "diencephalon" {
				t.Errorf("Unexpected cognitive_plan node direction: %s -> %s", ev.FromNode, ev.ToNode)
			}
		case "command_relay":
			hasCommandRelay = true
			t.Logf("[Collaboration Signal] From: %s, To: %s, EventType: %s, Desc: %q", ev.FromNode, ev.ToNode, ev.EventType, ev.Description)
			if ev.FromNode != "diencephalon" || ev.ToNode != "brainstem" {
				t.Errorf("Unexpected command_relay node direction: %s -> %s", ev.FromNode, ev.ToNode)
			}
		}
	}

	if !hasSensoryRelay {
		t.Error("Expected sensory_relay signal event")
	}
	if !hasCognitivePlan {
		t.Error("Expected cognitive_plan signal event")
	}
	if !hasCommandRelay {
		t.Error("Expected command_relay signal event")
	}
}
