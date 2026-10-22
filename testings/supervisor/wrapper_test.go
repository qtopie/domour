package supervisor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtopie/domour/ark/supervisor"
	"github.com/qtopie/domour/ark/supervisor/providers/antigravity"
	"github.com/qtopie/domour/ark/supervisor/providers/chatgpt"
)

// TestWrapCommandWithVProxy_ExecutionAndFallback [SPEC-SUPV-020]
func TestWrapCommandWithVProxy_ExecutionAndFallback(t *testing.T) {
	ctx := context.Background()
	targetBin := "echo"
	targetArgs := []string{"hello", "world"}

	// 1. 未开启 UseVProxy: 直接返回原生命令
	cmdNoProxy, cleanup1 := supervisor.WrapCommandWithVProxy(ctx, targetBin, targetArgs, supervisor.LaunchOptions{
		UseVProxy: false,
	})
	defer cleanup1()

	if strings.Contains(cmdNoProxy.Path, "vproxy") {
		t.Fatalf("expected direct command, got vproxy wrapper: %s", cmdNoProxy.Path)
	}
	if len(cmdNoProxy.Args) != 3 || cmdNoProxy.Args[0] != "echo" {
		t.Fatalf("unexpected args: %v", cmdNoProxy.Args)
	}

	// 2. 开启 UseVProxy 并提供有效 mock vproxy 二进制
	tmpDir := t.TempDir()
	mockVProxy := filepath.Join(tmpDir, "vproxy")
	script := "#!/bin/sh\nexec \"$@\"\n"
	if err := os.WriteFile(mockVProxy, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create mock vproxy: %v", err)
	}

	cmdWithProxy, cleanup2 := supervisor.WrapCommandWithVProxy(ctx, targetBin, targetArgs, supervisor.LaunchOptions{
		UseVProxy: true,
		VProxyBin: mockVProxy,
		Workspace: tmpDir,
	})
	defer cleanup2()

	if cmdWithProxy.Path != mockVProxy {
		t.Fatalf("expected vproxy path %s, got %s", mockVProxy, cmdWithProxy.Path)
	}
	if len(cmdWithProxy.Args) != 4 || cmdWithProxy.Args[0] != mockVProxy || cmdWithProxy.Args[1] != "echo" {
		t.Fatalf("expected wrapped args [vproxy echo hello world], got %v", cmdWithProxy.Args)
	}
	if cmdWithProxy.Dir != tmpDir {
		t.Fatalf("expected working dir %s, got %s", tmpDir, cmdWithProxy.Dir)
	}

	// 3. 开启 UseVProxy 但指定不存在的 vproxy: 优雅降级返回原生命令
	cmdFallback, cleanup3 := supervisor.WrapCommandWithVProxy(ctx, targetBin, targetArgs, supervisor.LaunchOptions{
		UseVProxy: true,
		VProxyBin: filepath.Join(tmpDir, "non_existent_vproxy_binary"),
	})
	defer cleanup3()

	if strings.Contains(cmdFallback.Path, "non_existent") {
		t.Fatalf("expected fallback to direct command, but kept non-existent binary: %s", cmdFallback.Path)
	}
}

// TestSupervisor_VProxyLifecycleIntegration [SPEC-SUPV-021]
func TestSupervisor_VProxyLifecycleIntegration(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// 创建可执行的 mock vproxy 脚本，将启动参数记录到 trace 文件
	traceFile := filepath.Join(tmpDir, "trace.log")
	mockVProxy := filepath.Join(tmpDir, "vproxy")
	script := "#!/bin/sh\necho \"WRAPPER_CALLED: $@\" >> " + traceFile + "\nexec \"$@\"\n"
	if err := os.WriteFile(mockVProxy, []byte(script), 0755); err != nil {
		t.Fatalf("failed to create mock vproxy: %v", err)
	}

	// 1. Controller LaunchHeadless 集成测试
	reg := supervisor.NewRegistry()
	ctrl := supervisor.NewController(reg)
	provider := chatgpt.NewProvider()

	mp, err := ctrl.LaunchHeadless(ctx, provider, "echo", supervisor.LaunchOptions{
		Workspace: tmpDir,
		UseVProxy: true,
		VProxyBin: mockVProxy,
		ExtraArgs: []string{"test-headless"},
	})
	if err != nil {
		t.Fatalf("LaunchHeadless failed: %v", err)
	}
	defer ctrl.StopProcess(mp.ID)

	// 校验子进程成功通过 mock vproxy 拉起
	_ = mp.Cmd.Wait()
	traceContent, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("trace file not found: %v", err)
	}
	if !strings.Contains(string(traceContent), "WRAPPER_CALLED") || !strings.Contains(string(traceContent), "echo") {
		t.Fatalf("expected trace to record vproxy execution, got: %s", string(traceContent))
	}

	// 2. Antigravity Provider SpawnSession 集成测试
	agyP := antigravity.NewProvider()
	spawnRes, err := agyP.SpawnSession(ctx, supervisor.SpawnRequest{
		Prompt:    "hello agy",
		Workspace: tmpDir,
		UseVProxy: true,
		VProxyBin: mockVProxy,
	})
	if err != nil {
		t.Fatalf("agy SpawnSession failed: %v", err)
	}
	if spawnRes == nil {
		t.Fatal("expected non-nil spawn result")
	}

	// 3. ChatGPT Provider SpawnSession 集成测试
	chatP := chatgpt.NewProvider()
	spawnResChat, err := chatP.SpawnSession(ctx, supervisor.SpawnRequest{
		Prompt:    "hello chatgpt",
		Workspace: tmpDir,
		UseVProxy: true,
		VProxyBin: mockVProxy,
	})
	if err != nil {
		t.Fatalf("chatgpt SpawnSession failed: %v", err)
	}
	if spawnResChat == nil {
		t.Fatal("expected non-nil spawn result")
	}
}
