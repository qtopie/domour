package supervisor_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
)

// TestSupervisor_RegisterAndDiscover [SPEC-SUPV-001]
func TestSupervisor_RegisterAndDiscover(t *testing.T) {
	sockPath := filepath.Join(os.TempDir(), "domour-test-supv-1.sock")
	defer os.Remove(sockPath)

	supv, err := supervisor.New(supervisor.Config{
		SocketPath: sockPath,
	})
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}
	defer supv.Stop()

	agyProvider := antigravity.NewProvider()
	if err := supv.RegisterProvider(agyProvider); err != nil {
		t.Fatalf("failed to register provider: %v", err)
	}

	p, ok := supv.GetProvider(antigravity.ProviderName)
	if !ok || p == nil {
		t.Fatalf("expected provider %s to be registered", antigravity.ProviderName)
	}

	// 模拟接收首次心跳
	fixturePath := filepath.Join("..", "..", "harness", "fixtures", "supervisor", "agy_statusline.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	update, err := agyProvider.ParseStatusline(data)
	if err != nil {
		t.Fatalf("failed to parse statusline: %v", err)
	}

	if update.ID != "test-conv-12345" {
		t.Errorf("expected ID test-conv-12345, got %s", update.ID)
	}

	// 注册到 Supervisor Registry 中
	reg := supv.Registry()
	proc := reg.UpdateFromStatusline(update, agyProvider.Name())

	if proc.ID != "test-conv-12345" {
		t.Errorf("expected process ID test-conv-12345, got %s", proc.ID)
	}
	if proc.Provider != antigravity.ProviderName {
		t.Errorf("expected provider antigravity, got %s", proc.Provider)
	}

	list := supv.ListProcesses()
	if len(list) != 1 {
		t.Fatalf("expected 1 process in registry, got %d", len(list))
	}
}

// TestSupervisor_ToolPendingDetection [SPEC-SUPV-002]
func TestSupervisor_ToolPendingDetection(t *testing.T) {
	reg := supervisor.NewRegistry()
	agyProvider := antigravity.NewProvider()

	eventCh := make(chan supervisor.ProcessEvent, 10)
	reg.Subscribe(eventCh)

	// 模拟正常工作状态
	update1 := &supervisor.StatuslineUpdate{
		ID:          "proc-tool-test",
		AgentState:  "thinking",
		ToolPending: false,
	}
	proc1 := reg.UpdateFromStatusline(update1, agyProvider.Name())
	if proc1.Status != supervisor.StatusThinking {
		t.Errorf("expected status StatusThinking, got %s", proc1.Status)
	}

	// 收到工具确认等待心跳
	update2 := &supervisor.StatuslineUpdate{
		ID:          "proc-tool-test",
		AgentState:  "tool_use",
		ToolPending: true,
		CurrentTool: "run_command",
	}
	proc2 := reg.UpdateFromStatusline(update2, agyProvider.Name())
	if proc2.Status != supervisor.StatusAwaitingConfirmation {
		t.Errorf("expected status StatusAwaitingConfirmation, got %s", proc2.Status)
	}
	if !proc2.ToolPending {
		t.Errorf("expected ToolPending true")
	}

	// 检验是否收到事件
	select {
	case evt := <-eventCh:
		if evt.Process.ID != "proc-tool-test" {
			t.Errorf("unexpected event process ID: %s", evt.Process.ID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for process event")
	}
}
