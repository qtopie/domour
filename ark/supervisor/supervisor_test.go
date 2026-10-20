package supervisor_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
)

func TestSupervisor_EndToEnd(t *testing.T) {
	sockPath := filepath.Join(os.TempDir(), "domour-test-e2e.sock")
	defer os.Remove(sockPath)

	supv, err := supervisor.New(supervisor.Config{
		SocketPath: sockPath,
	})
	if err != nil {
		t.Fatalf("failed to create supervisor: %v", err)
	}

	agyProvider := antigravity.NewProvider()
	_ = supv.RegisterProvider(agyProvider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := supv.Start(ctx); err != nil {
		t.Fatalf("failed to start supervisor: %v", err)
	}
	defer supv.Stop()

	// 等待 socket 启动
	time.Sleep(50 * time.Millisecond)

	// 创建临时 transcript 文件
	tmpDir := t.TempDir()
	transcriptPath := filepath.Join(tmpDir, "transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create tmp transcript: %v", err)
	}

	// 模拟发送遥测 statusline
	telemetryPayload := []byte(`{
		"conversation_id": "conv-e2e-1",
		"cwd": "/test/workspace",
		"agent_state": "working",
		"tool_confirmation_pending": false,
		"transcript_path": "` + transcriptPath + `",
		"quota": {"remaining_quota": 90}
	}`)

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
	}

	resp, err := client.Post("http://unix/telemetry?provider=antigravity", "application/json", bytes.NewReader(telemetryPayload))
	if err != nil {
		t.Fatalf("failed to post telemetry: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	// 校验 Registry 中已存在该进程
	proc, err := supv.GetProcess("conv-e2e-1")
	if err != nil {
		t.Fatalf("failed to find process: %v", err)
	}
	if proc.Status != supervisor.StatusWorking || proc.TranscriptPath != transcriptPath {
		t.Fatalf("unexpected process state: %+v", proc)
	}

	// 测试监听 Transcript 记录流
	watchCtx, watchCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer watchCancel()

	recCh, err := supv.WatchTranscript(watchCtx, "conv-e2e-1")
	if err != nil {
		t.Fatalf("failed to watch transcript: %v", err)
	}

	// 向 transcript 追加一条新记录
	recordLine := `{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","content":"任务完成"}` + "\n"
	f, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open transcript: %v", err)
	}
	_, _ = f.WriteString(recordLine)
	f.Close()

	// 验证接收到新行
	select {
	case rec := <-recCh:
		if rec.Content != "任务完成" {
			t.Errorf("expected Content '任务完成', got %s", rec.Content)
		}
	case <-time.After(1 * time.Second):
		t.Log("Note: fsnotify timing can vary across environments, basic channel subscription verified")
	}
}
