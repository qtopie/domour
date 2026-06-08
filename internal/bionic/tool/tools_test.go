package tool

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerExecutesInternalTool(t *testing.T) {
	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	if err := manager.Register(NewInternalTool("internal.echo", "echo", func(ctx context.Context, command Command) (Result, error) {
		return Result{Observation: command.Input["value"].(string), Done: true}, nil
	})); err != nil {
		t.Fatalf("register internal tool: %v", err)
	}

	result, err := manager.Execute(context.Background(), Command{
		ID:     "cmd-1",
		Action: "internal.echo",
		Input: map[string]interface{}{
			"value": "hello",
		},
	})
	if err != nil {
		t.Fatalf("execute internal tool: %v", err)
	}
	if result.Observation != "hello" {
		t.Fatalf("unexpected observation: %q", result.Observation)
	}
	if result.Meta["kind"] != string(ToolKindInternal) {
		t.Fatalf("unexpected kind meta: %#v", result.Meta)
	}
}

func TestManagerExecutesCLITool(t *testing.T) {
	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	if err := manager.Register(NewShellTool("shell.exec", "shell")); err != nil {
		t.Fatalf("register shell tool: %v", err)
	}

	command := "printf cli-ok"
	if runtime.GOOS == "windows" {
		command = "echo cli-ok"
	}

	result, err := manager.Execute(context.Background(), Command{
		Action: "shell.exec",
		Input: map[string]interface{}{
			"command": command,
		},
	})
	if err != nil {
		t.Fatalf("execute shell tool: %v", err)
	}
	if got := result.Observation; got == "" {
		t.Fatalf("expected shell output, got empty string")
	}
}

func TestManagerUnloadsIdleGRPCTool(t *testing.T) {
	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	var closed atomic.Int32
	spec := NewGRPCTool("grpc.echo", "grpc", func(context.Context) (GRPCToolClient, error) {
		return &fakeGRPCClient{closed: &closed}, nil
	})
	spec.IdleTTL = time.Millisecond
	if err := manager.Register(spec); err != nil {
		t.Fatalf("register grpc tool: %v", err)
	}

	result, err := manager.Execute(context.Background(), Command{Action: "grpc.echo"})
	if err != nil {
		t.Fatalf("execute grpc tool: %v", err)
	}
	if result.Observation != "grpc-ok" {
		t.Fatalf("unexpected grpc observation: %q", result.Observation)
	}

	time.Sleep(5 * time.Millisecond)
	if err := manager.UnloadIdle(context.Background()); err != nil {
		t.Fatalf("unload idle: %v", err)
	}
	if closed.Load() != 1 {
		t.Fatalf("expected grpc client to close once, got %d", closed.Load())
	}
}

func TestManagerExecutesMCPTool(t *testing.T) {
	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	if err := manager.Register(NewMCPTool("mcp.todo.list", "todo.list", "mcp", func(context.Context) (MCPToolClient, error) {
		return fakeMCPClient{}, nil
	})); err != nil {
		t.Fatalf("register mcp tool: %v", err)
	}

	result, err := manager.Execute(context.Background(), Command{
		Action: "mcp.todo.list",
		Input: map[string]interface{}{
			"status": "open",
		},
	})
	if err != nil {
		t.Fatalf("execute mcp tool: %v", err)
	}
	if result.Observation != "mcp:todo.list" {
		t.Fatalf("unexpected mcp observation: %q", result.Observation)
	}
}

type fakeGRPCClient struct {
	closed *atomic.Int32
}

func (f *fakeGRPCClient) Invoke(context.Context, Command) (Result, error) {
	return Result{Observation: "grpc-ok", Done: true}, nil
}

func (f *fakeGRPCClient) Close(context.Context) error {
	f.closed.Add(1)
	return nil
}

type fakeMCPClient struct{}

func (fakeMCPClient) CallTool(_ context.Context, name string, _ map[string]interface{}) (MCPCallResult, error) {
	return MCPCallResult{
		Content: "mcp:" + name,
		Meta: map[string]string{
			"remote_tool": name,
		},
	}, nil
}

func (fakeMCPClient) Close(context.Context) error {
	return nil
}

func TestManagerExecutesRuntimeInfoTool(t *testing.T) {
	manager, err := NewDefaultManager()
	if err != nil {
		t.Fatalf("failed to create default manager: %v", err)
	}
	defer manager.Close()

	result, err := manager.Execute(context.Background(), Command{
		Action: "runtime.info",
	})
	if err != nil {
		t.Fatalf("failed to execute runtime.info tool: %v", err)
	}

	if result.Observation == "" {
		t.Fatalf("expected non-empty observation from runtime.info")
	}

	if !strings.Contains(result.Observation, "go_version") {
		t.Fatalf("expected observation to contain 'go_version'")
	}
}
