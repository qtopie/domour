package supervisor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtopie/domour/ark/supervisor"
	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
)

// TestAntigravityProvider_ParseStatusline [SPEC-SUPV-003]
func TestAntigravityProvider_ParseStatusline(t *testing.T) {
	provider := antigravity.NewProvider()

	fixturePath := filepath.Join("..", "..", "harness", "fixtures", "supervisor", "agy_statusline.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	update, err := provider.ParseStatusline(data)
	if err != nil {
		t.Fatalf("failed to parse statusline: %v", err)
	}

	if update.ID != "test-conv-12345" {
		t.Errorf("expected ID test-conv-12345, got %s", update.ID)
	}
	if update.Workspace != "/home/qtopierw/workspace/projects/domour" {
		t.Errorf("unexpected workspace: %s", update.Workspace)
	}
	if update.AgentState != "tool_use" {
		t.Errorf("unexpected agent_state: %s", update.AgentState)
	}
	if !update.ToolPending {
		t.Errorf("expected ToolPending true")
	}
	if update.CurrentTool != "run_command" {
		t.Errorf("unexpected CurrentTool: %s", update.CurrentTool)
	}
	if update.ContextPercent != 35.0 {
		t.Errorf("expected ContextPercent 35.0, got %f", update.ContextPercent)
	}
	if update.QuotaRemaining != 85 {
		t.Errorf("expected QuotaRemaining 85, got %d", update.QuotaRemaining)
	}
}

// TestAntigravityProvider_ParseTranscript [SPEC-SUPV-004]
func TestAntigravityProvider_ParseTranscript(t *testing.T) {
	provider := antigravity.NewProvider()

	// 模拟解析单条用户输入
	userLine := []byte(`{"step_index":1,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-10-15T10:00:00Z","content":"请检查代码状态并构建项目"}`)
	rec1, err := provider.ParseTranscriptLine(userLine)
	if err != nil {
		t.Fatalf("failed to parse user transcript line: %v", err)
	}
	if rec1.Role != "user" || rec1.Content != "请检查代码状态并构建项目" || rec1.StepIndex != 1 {
		t.Errorf("unexpected user record: %+v", rec1)
	}

	// 模拟解析模型响应（带 ToolCall 与 Thinking）
	modelLine := []byte(`{"step_index":2,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-15T10:00:05Z","content":"好的，我来先检查 git 状态。","thinking":"需要先确认当前工作区是否干净。","tool_calls":[{"tool_name":"run_command","arguments":{"CommandLine":"git status"},"summary":"Git status check"}]}`)
	rec2, err := provider.ParseTranscriptLine(modelLine)
	if err != nil {
		t.Fatalf("failed to parse model transcript line: %v", err)
	}

	if rec2.Role != "assistant" {
		t.Errorf("expected assistant role, got %s", rec2.Role)
	}
	if rec2.Thinking != "需要先确认当前工作区是否干净。" {
		t.Errorf("unexpected thinking: %s", rec2.Thinking)
	}
	if len(rec2.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(rec2.ToolCalls))
	}
	if rec2.ToolCalls[0].ToolName != "run_command" {
		t.Errorf("expected tool_name run_command, got %s", rec2.ToolCalls[0].ToolName)
	}

	// 测试生成 Headless 与 Resume 命令参数
	headlessArgs, err := provider.BuildHeadlessArgs(supervisor.LaunchOptions{
		Workspace:                  "/test/dir",
		SessionID:                  "sess-1",
		DangerouslySkipPermissions: true,
	})
	if err != nil {
		t.Fatalf("failed to build headless args: %v", err)
	}
	if len(headlessArgs) < 5 {
		t.Errorf("unexpected headless args: %v", headlessArgs)
	}

	resumeArgs, err := provider.BuildResumeArgs("sess-1", "继续执行", supervisor.LaunchOptions{
		Workspace: "/test/dir",
	})
	if err != nil {
		t.Fatalf("failed to build resume args: %v", err)
	}
	if resumeArgs[0] != "--conversation" || resumeArgs[1] != "sess-1" {
		t.Errorf("unexpected resume args: %v", resumeArgs)
	}
}

// TestAntigravityProvider_DiscoverRunningSessions [SPEC-SUPV-008]
func TestAntigravityProvider_DiscoverRunningSessions(t *testing.T) {
	sessions, err := antigravity.DiscoverRunningSessions()
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}

	// 只要当前机器上有运行 agy，至少会发现包含当前会话或其它会话
	t.Logf("Discovered %d active agy sessions", len(sessions))
	for _, s := range sessions {
		if s.Process.ID == "" {
			t.Errorf("session ID should not be empty")
		}
		if s.Process.PID <= 0 {
			t.Errorf("session PID should be positive")
		}
		if s.Process.Provider != antigravity.ProviderName {
			t.Errorf("provider should be %s, got %s", antigravity.ProviderName, s.Process.Provider)
		}
		if s.Process.ClientType == "" {
			t.Errorf("session client_type should not be empty")
		}
	}
}

// TestAntigravityProvider_IDEDualVariant [SPEC-SUPV-010]
func TestAntigravityProvider_IDEDualVariant(t *testing.T) {
	// 验证 ClientType 判定
	cliPath := "/home/user/.gemini/antigravity-cli/brain/abc-123/.system_generated/logs/transcript.jsonl"
	if ct := antigravity.DetectClientType(cliPath); ct != "cli" {
		t.Errorf("expected cli, got %s", ct)
	}

	idePath1 := "/home/user/.gemini/antigravity/brain/xyz-789/.system_generated/logs/transcript.jsonl"
	if ct := antigravity.DetectClientType(idePath1); ct != "ide" {
		t.Errorf("expected ide, got %s", ct)
	}

	idePath2 := "C:\\Users\\qtopie\\.gemini\\antigravity\\brain\\win-session\\.system_generated\\logs\\transcript.jsonl"
	if ct := antigravity.DetectClientType(idePath2); ct != "ide" {
		t.Errorf("expected ide, got %s", ct)
	}
}

// TestAntigravityProvider_IDEInstructionDispatch [SPEC-SUPV-011]
func TestAntigravityProvider_IDEInstructionDispatch(t *testing.T) {
	// 验证能否定位 AgentAPI 或 IDE 二进制
	apiExe, isSubcmd, ok := antigravity.FindAgentAPI()
	t.Logf("FindAgentAPI: exe=%s isSubcmd=%v ok=%v", apiExe, isSubcmd, ok)

	ideExe, ideOk := antigravity.FindIDEExecutable()
	t.Logf("FindIDEExecutable: exe=%s ok=%v", ideExe, ideOk)

	// 在安装有 Antigravity 的系统上，至少能探测到 agentapi 脚本或 language_server 或 IDE
	if !ok && !ideOk {
		t.Logf("Note: neither agentapi nor IDE binary found in this build container/environment")
	}
}
