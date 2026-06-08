package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/cognitor/proxy"
)

type mockDiencephalonClient struct {
	t          *testing.T
	step       int
	toolResult string
}

func (m *mockDiencephalonClient) Provider() string { return "mock-provider" }
func (m *mockDiencephalonClient) Model() string    { return "mock-model" }
func (m *mockDiencephalonClient) Type() string     { return "api" }
func (m *mockDiencephalonClient) IsReady(ctx context.Context) (bool, error) { return true, nil }

func (m *mockDiencephalonClient) GenerateMessage(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	m.step++
	if m.step == 1 {
		// 1. Diencephalon Routing & Brain Analysis (间脑路由与大脑分析)
		// Brain receives "Calculate 6 * 7" and decides to call the calculator tool.
		toolCall := schema.ToolCall{
			ID: "tc-calculator-6x7",
			Function: schema.FunctionCall{
				Name:      "calculator",
				Arguments: `{"expression": "6 * 7"}`,
			},
		}
		return &schema.Message{
			Role:      schema.Assistant,
			Content:   "I will use the calculator tool to evaluate 6 * 7.",
			ToolCalls: []schema.ToolCall{toolCall},
		}, nil
	} else if m.step == 2 {
		// 5. Final Answer Generation (回答最终答案)
		// Brain receives tool output (42) and explains the result.
		var foundToolResult bool
		for _, msg := range messages {
			if msg.Role == schema.Tool && msg.ToolCallID == "tc-calculator-6x7" {
				m.toolResult = msg.Content
				foundToolResult = true
			}
		}
		if !foundToolResult {
			m.t.Errorf("Expected tool result message in history but not found")
		}
		return &schema.Message{
			Role:    schema.Assistant,
			Content: fmt.Sprintf("The calculated answer to 6 * 7 is %s.", m.toolResult),
		}, nil
	}
	return nil, fmt.Errorf("unexpected step: %d", m.step)
}

func (m *mockDiencephalonClient) GenerateText(ctx context.Context, messages []*schema.Message) (proxy.Response, error) {
	msg, err := m.GenerateMessage(ctx, messages)
	if err != nil {
		return proxy.Response{}, err
	}
	return proxy.Response{
		Content:  msg.Content,
		Provider: m.Provider(),
		Model:    m.Model(),
	}, nil
}

func (m *mockDiencephalonClient) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

type testExecutorClient struct {
	manager *tool.Manager
}

func (m *testExecutorClient) Execute(ctx context.Context, cmd tool.Command) (tool.Result, error) {
	return m.manager.Execute(ctx, cmd)
}

func (m *testExecutorClient) Veto(ctx context.Context, action string) bool {
	return false
}

func (m *testExecutorClient) ListTools(ctx context.Context) ([]tool.ToolInfo, error) {
	return m.manager.List(), nil
}

func TestCalculatorReasoningLoop(t *testing.T) {
	ctx := context.Background()

	// 1. Diencephalon Gateway Client Mock (间脑路由模拟)
	mockLLM := &mockDiencephalonClient{t: t}

	// 2. Cerebellum / Tool Manager Setup (小脑肌肉记忆查询设置)
	manager := tool.NewManager()

	calculatorSpec := tool.NewInternalTool("calculator", "Evaluate simple math expression", func(ctx context.Context, cmd tool.Command) (tool.Result, error) {
		expr, ok := cmd.Input["expression"].(string)
		if !ok {
			return tool.Result{}, fmt.Errorf("missing expression parameter")
		}

		// 4. Physical Tool Call / Motor (工具执行层/脑干)
		var result string
		exprClean := strings.ReplaceAll(expr, " ", "")
		if exprClean == "6*7" || exprClean == "6x7" || exprClean == "6\\*7" {
			result = "42"
		} else {
			result = "Unknown math expression: " + expr
		}

		return tool.Result{
			CommandID:   cmd.ID,
			Observation: result,
			Done:        true,
		}, nil
	})

	if err := manager.Register(calculatorSpec); err != nil {
		t.Fatalf("failed to register calculator tool: %v", err)
	}

	// Ensure Cerebellum can query and resolve the tool (小脑肌肉记忆查询验证)
	tools := manager.List()
	var hasCalculator bool
	for _, tInfo := range tools {
		if tInfo.Name == "calculator" {
			hasCalculator = true
			break
		}
	}
	if !hasCalculator {
		t.Fatalf("calculator tool was not found in registered tools")
	}

	executorClient := &testExecutorClient{manager: manager}

	// 3. Initiate the workflow simulation
	history := []*schema.Message{
		schema.UserMessage("What is 6 * 7?"),
	}

	// 大脑分析 (Brain Analysis) -> Propose Tool Call
	brainMsg, err := mockLLM.GenerateMessage(ctx, history)
	if err != nil {
		t.Fatalf("brain thinking step 1 failed: %v", err)
	}

	if len(brainMsg.ToolCalls) == 0 {
		t.Fatalf("expected brain to return tool calls, but got none")
	}

	// Cerebellum handles routing and executes the proposed motor actions
	history = append(history, brainMsg)
	for _, tc := range brainMsg.ToolCalls {
		if tc.Function.Name != "calculator" {
			t.Fatalf("unexpected tool call proposed: %s", tc.Function.Name)
		}

		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			t.Fatalf("failed to parse tool call arguments: %v", err)
		}

		// Execute via ExecutorClient
		cmd := tool.Command{
			ID:     tc.ID,
			Action: tc.Function.Name,
			Input:  args,
		}

		res, err := executorClient.Execute(ctx, cmd)
		if err != nil {
			t.Fatalf("failed to execute motor action: %v", err)
		}

		if res.Observation != "42" {
			t.Errorf("expected tool observation to be '42', but got: %q", res.Observation)
		}

		toolMsg := &schema.Message{
			Role:       schema.Tool,
			Content:    res.Observation,
			ToolCallID: tc.ID,
		}
		history = append(history, toolMsg)
	}

	// 回答答案 (Brain completes loop and replies to user)
	finalReply, err := mockLLM.GenerateMessage(ctx, history)
	if err != nil {
		t.Fatalf("brain thinking step 2 failed: %v", err)
	}

	t.Logf("Final reply: %s", finalReply.Content)

	if !strings.Contains(finalReply.Content, "42") {
		t.Errorf("expected answer to contain 42, but got: %q", finalReply.Content)
	}
}
