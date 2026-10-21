package supervisor_test

import (
	"context"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
)

// TestController_ToolApproval [SPEC-SUPV-005]
func TestController_ToolApproval(t *testing.T) {
	reg := supervisor.NewRegistry()
	ctrl := supervisor.NewController(reg)

	processID := "test-proc-approval"
	toolName := "run_command"

	// 模拟异步 Hook 等待审批
	decisionCh := make(chan supervisor.ToolDecision, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		dec, _ := ctrl.WaitForDecision(ctx, processID, toolName, 2*time.Second)
		decisionCh <- dec
	}()

	// 确认 Registry 标记为 Pending
	time.Sleep(50 * time.Millisecond)
	proc, ok := reg.Get(processID)
	if ok && !proc.ToolPending {
		t.Errorf("expected process ToolPending to be true")
	}

	// 外部提交审批放行
	if err := ctrl.SubmitToolDecision(processID, supervisor.DecisionAllow, "Approved by admin"); err != nil {
		t.Fatalf("failed to submit tool decision: %v", err)
	}

	select {
	case dec := <-decisionCh:
		if dec != supervisor.DecisionAllow {
			t.Errorf("expected decision allow, got %s", dec)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for hook decision")
	}
}

// TestController_SendInstructionStream [SPEC-SUPV-006]
func TestController_SendInstructionStream(t *testing.T) {
	reg := supervisor.NewRegistry()
	ctrl := supervisor.NewController(reg)
	provider := antigravity.NewProvider()

	// 用轻量级 echo/cat 命令模拟 Headless 子进程（跨平台兼容 bash 或 cat）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "cat")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("failed to create stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to create stdout: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start mock process: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
	}()

	mp := &supervisor.ManagedProcess{
		ID:     "managed-1",
		Cmd:    cmd,
		Stdin:  stdin,
		Stdout: stdout,
		Cancel: cancel,
	}

	// 注册到 controller
	ctrl.RegisterManagedProcess(mp)

	// 发送指令
	if err := ctrl.SendInstruction(ctx, "managed-1", "请继续执行单元测试"); err != nil {
		t.Fatalf("failed to send instruction: %v", err)
	}

	// 校验从 cat 读取的响应
	buf := make([]byte, 1024)
	n, err := stdout.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("failed to read from mock stdout: %v", err)
	}

	output := string(buf[:n])
	if !containsStr(output, "请继续执行单元测试") {
		t.Errorf("expected instruction in stdout, got: %s", output)
	}

	_ = provider
}

func containsStr(s, substr string) bool {
	return stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
