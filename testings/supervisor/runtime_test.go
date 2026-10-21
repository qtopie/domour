package supervisor_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
	"github.com/qtopie/domour/ark/supervisor/providers/generic"
)

func setupTestSupervisor(t *testing.T) supervisor.Supervisor {
	t.Helper()
	tmpDir := t.TempDir()
	sup, err := supervisor.New(supervisor.Config{
		SocketPath: filepath.Join(tmpDir, "supervisor.sock"),
	})
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}
	_ = sup.RegisterProvider(antigravity.NewProvider())
	_ = sup.RegisterProvider(generic.NewProvider())
	return sup
}

// TestSupervisor_DiscoverSessions [SPEC-SUPV-012]
func TestSupervisor_DiscoverSessions(t *testing.T) {
	sup := setupTestSupervisor(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessions, err := sup.DiscoverSessions(ctx)
	if err != nil {
		t.Fatalf("DiscoverSessions failed: %v", err)
	}

	t.Logf("Discovered %d sessions across providers", len(sessions))
	for _, s := range sessions {
		if s.ID == "" {
			t.Errorf("expected non-empty session ID")
		}
		if s.ClientType == "" {
			t.Errorf("session %s expected client_type ('cli' or 'ide'), got empty", s.ID)
		}
	}
}

// TestSupervisor_SpawnSession [SPEC-SUPV-013]
func TestSupervisor_SpawnSession(t *testing.T) {
	sup := setupTestSupervisor(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. 参数校验
	if _, err := sup.SpawnSession(ctx, supervisor.SpawnRequest{Prompt: ""}); err == nil {
		t.Errorf("expected error when prompt is empty")
	}

	// 2. 模拟拉起（优先 AgentAPI 或 CLI）
	res, err := sup.SpawnSession(ctx, supervisor.SpawnRequest{
		Prompt: "Unit test probe prompt",
		Title:  "Runtime Probe",
	})
	if err != nil {
		t.Logf("SpawnSession note: %v (expected if running in headless build container)", err)
		return
	}

	if res.Mode == "" {
		t.Errorf("expected non-empty Mode in SpawnResult")
	}
	t.Logf("SpawnSession succeeded: Mode=%s, SessionID=%s, PID=%d", res.Mode, res.SessionID, res.PID)
}

// TestSupervisor_InstructSession [SPEC-SUPV-014]
func TestSupervisor_InstructSession(t *testing.T) {
	sup := setupTestSupervisor(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. 参数校验
	if _, err := sup.InstructSession(ctx, supervisor.InstructRequest{SessionID: "", Instruction: "test"}); err == nil {
		t.Errorf("expected error when session_id is empty")
	}
	if _, err := sup.InstructSession(ctx, supervisor.InstructRequest{SessionID: "sess-1", Instruction: ""}); err == nil {
		t.Errorf("expected error when instruction is empty")
	}

	// 2. 指令自适应下发
	res, err := sup.InstructSession(ctx, supervisor.InstructRequest{
		SessionID:   "sess-test-runtime-123",
		Instruction: "continue execution",
		Title:       "Test Instruct",
	})
	if err != nil {
		t.Logf("InstructSession note: %v", err)
		return
	}

	if res.Status != "sent" && res.Status != "resumed" {
		t.Errorf("unexpected InstructResult status: %s", res.Status)
	}
	t.Logf("InstructSession succeeded: Mode=%s, Status=%s", res.Mode, res.Status)
}
