package supervisor_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/qtopie/domour/ark/supervisor"
	"github.com/qtopie/domour/ark/supervisor/providers/chatgpt"
)

// TestChatGPTProvider_ParseTranscriptLine [SPEC-SUPV-018]
func TestChatGPTProvider_ParseTranscriptLine(t *testing.T) {
	provider := chatgpt.NewProvider()

	// 1. 测试普通助手消息
	lineMsg := []byte(`{"type":"agent_message","role":"assistant","content":"已经定位到问题所在"}`)
	rec, err := provider.ParseTranscriptLine(lineMsg)
	if err != nil {
		t.Fatalf("unexpected error parsing agent_message: %v", err)
	}
	if rec.Role != "assistant" || rec.Content != "已经定位到问题所在" {
		t.Errorf("unexpected record: %+v", rec)
	}

	// 2. 测试工具调用
	lineTool := []byte(`{"type":"tool_call","name":"execute_command","arguments":{"command":"git status"}}`)
	recTool, err := provider.ParseTranscriptLine(lineTool)
	if err != nil {
		t.Fatalf("unexpected error parsing tool_call: %v", err)
	}
	if recTool.Type != "tool_use" || len(recTool.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(recTool.ToolCalls))
	}
	if recTool.ToolCalls[0].ToolName != "execute_command" {
		t.Errorf("expected execute_command, got %s", recTool.ToolCalls[0].ToolName)
	}

	// 3. 测试轮次完成与 token 统计
	lineComplete := []byte(`{"type":"turn_completed","tokens_used":1250}`)
	recComplete, err := provider.ParseTranscriptLine(lineComplete)
	if err != nil {
		t.Fatalf("unexpected error parsing turn_completed: %v", err)
	}
	if recComplete.Type != "turn_completed" {
		t.Errorf("expected type turn_completed, got %s", recComplete.Type)
	}

	// 4. 测试 Codex 官方原生真实事件 (dot-separated)
	lineThread := []byte(`{"type":"thread.started","thread_id":"01a15f05-c19a-7482-9d62-7f16d263a454"}`)
	recThread, err := provider.ParseTranscriptLine(lineThread)
	if err != nil {
		t.Fatalf("failed to parse thread.started: %v", err)
	}
	if recThread.Type != "thread_started" || recThread.Content != "01a15f05-c19a-7482-9d62-7f16d263a454" {
		t.Errorf("unexpected thread record: %+v", recThread)
	}

	lineErr := []byte(`{"type":"error","message":"Reconnecting... 5/5 (request timed out)"}`)
	recErr, err := provider.ParseTranscriptLine(lineErr)
	if err != nil {
		t.Fatalf("failed to parse error line: %v", err)
	}
	if recErr.Type != "error" || recErr.Content != "Reconnecting... 5/5 (request timed out)" {
		t.Errorf("unexpected error record: %+v", recErr)
	}
}

// TestChatGPTProvider_DiscoverSessions [SPEC-SUPV-016]
func TestChatGPTProvider_DiscoverSessions(t *testing.T) {
	fixtureDB := filepath.Join("..", "..", "harness", "fixtures", "supervisor", "chatgpt", "threads.sqlite")
	if _, err := os.Stat(fixtureDB); err != nil {
		t.Fatalf("missing sqlite fixture: %v", err)
	}

	provider := chatgpt.NewProviderWithOptions(chatgpt.Options{
		DBPath: fixtureDB,
	})

	sessions, err := provider.DiscoverSessions()
	if err != nil {
		t.Fatalf("failed to discover sessions: %v", err)
	}

	if len(sessions) < 2 {
		t.Fatalf("expected at least 2 sessions from fixture, got %d", len(sessions))
	}

	var foundCodex, foundChat bool
	for _, s := range sessions {
		if s.Provider != "chatgpt" {
			t.Errorf("expected provider chatgpt, got %s", s.Provider)
		}
		if s.ID == "thr_test_codex_001" {
			foundCodex = true
			if s.Workspace != "/home/qtopierw/workspace/projects/domour" {
				t.Errorf("unexpected workspace: %s", s.Workspace)
			}
		}
		if s.ID == "thr_test_chat_002" {
			foundChat = true
		}
	}

	if !foundCodex || !foundChat {
		t.Errorf("expected to find both test sessions, foundCodex=%v, foundChat=%v", foundCodex, foundChat)
	}
}

// TestChatGPTProvider_MatchAndInspect [SPEC-SUPV-017]
func TestChatGPTProvider_MatchAndInspect(t *testing.T) {
	fixtureDB := filepath.Join("..", "..", "harness", "fixtures", "supervisor", "chatgpt", "threads.sqlite")
	rolloutFixture := filepath.Join("..", "..", "harness", "fixtures", "supervisor", "chatgpt", "rollout_sample.jsonl")

	provider := chatgpt.NewProviderWithOptions(chatgpt.Options{
		DBPath: fixtureDB,
	})

	// 测试匹配会话
	targetID := "thr_test_codex_001"
	path, ok := provider.MatchSession(targetID)
	if !ok {
		t.Fatalf("expected session %s to match", targetID)
	}
	if path == "" {
		t.Fatalf("expected non-empty path for session %s", targetID)
	}

	// 使用 rollout 夹具测试多轮回溯
	turns, err := chatgpt.ReadTurnsFromRollout(rolloutFixture, 2)
	if err != nil {
		t.Fatalf("failed to read turns from rollout: %v", err)
	}

	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if turns[1].Content != "项目结构完整，现在生成重构报告。" {
		t.Errorf("unexpected last turn content: %s", turns[1].Content)
	}
}

// TestChatGPTProvider_AppServerRPC [SPEC-SUPV-019]
func TestChatGPTProvider_AppServerRPC(t *testing.T) {
	// 测试 ThreadStartRequest JSON-RPC 2.0 序列化
	req := chatgpt.NewJSONRPCRequest(1, "thread/start", map[string]interface{}{
		"cwd":   "/home/qtopierw/workspace/projects/domour",
		"model": "o3",
	})

	bytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal jsonrpc request: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(bytes, &raw); err != nil {
		t.Fatalf("failed to unmarshal jsonrpc request: %v", err)
	}

	if raw["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %v", raw["jsonrpc"])
	}
	if raw["method"] != "thread/start" {
		t.Errorf("expected method thread/start, got %v", raw["method"])
	}

	// 测试 Approval 响应格式化
	hookReq := &supervisor.HookRequest{
		ToolName:   "execute_command",
		ToolCallID: "call_abc123",
		ProcessID:  "thr_test_codex_001",
	}

	respBytes, err := chatgpt.NewProvider().FormatHookDecision(hookReq, supervisor.DecisionAllow, "")
	if err != nil {
		t.Fatalf("failed to format hook decision: %v", err)
	}

	var decMap map[string]interface{}
	if err := json.Unmarshal(respBytes, &decMap); err != nil {
		t.Fatalf("failed to unmarshal decision response: %v", err)
	}

	if decMap["decision"] != "allow" {
		t.Errorf("expected decision allow, got %v", decMap["decision"])
	}
}
