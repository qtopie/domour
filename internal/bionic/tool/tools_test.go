package tool

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	publicskill "github.com/qtopie/domour/ark/skill"
	publictool "github.com/qtopie/domour/ark/tool"
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

func TestLazySkillToolFiltering(t *testing.T) {
	rawSkill := `---
id: "plugin:sample"
name: "SamplePlugin"
description: "Sample plugin skill for lazy tool gating"
intent_tags:
  - "sample_tag"
  - "专属标签"
tools:
  - name: "sample.do_work"
    description: "Perform sample plugin work"
    parameters:
      type: object
      properties:
        project:
          type: string
---
# Sample Skill Instructions
Use sample.do_work to handle tasks.
`
	parsed, err := publicskill.ParseSkill(rawSkill)
	if err != nil {
		t.Fatalf("ParseSkill failed: %v", err)
	}
	if len(parsed.Tools) != 1 || parsed.Tools[0].Name != "sample.do_work" {
		t.Fatalf("expected parsed skill to preserve tool definition, got %+v", parsed.Tools)
	}
	publicskill.ReplaceByName(parsed)
	publictool.Register(publictool.Tool{
		Name:        "sample.do_work",
		Description: "Perform sample plugin work",
		Parameters:  string(parsed.Tools[0].Parameters),
		Act: func(ctx context.Context, input map[string]any) (string, error) {
			return "work-done", nil
		},
	})

	manager, err := NewDefaultManager()
	if err != nil {
		t.Fatalf("NewDefaultManager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// 1. Before skill activation, List() contains sample.do_work, but ListActiveTools hides it.
	allTools := manager.List()
	hasSampleInAll := false
	for _, ti := range allTools {
		if ti.Name == "sample.do_work" {
			hasSampleInAll = true
			if ti.Params == nil {
				t.Fatalf("expected JSON schema parameters to be converted into Eino ParamsOneOf")
			}
		}
	}
	if !hasSampleInAll {
		t.Fatalf("expected sample.do_work in List()")
	}

	activeTools := manager.ListActiveTools(ctx)
	for _, ti := range activeTools {
		if ti.Name == "sample.do_work" {
			t.Fatalf("expected sample.do_work to be gated (excluded) when SamplePlugin skill is inactive")
		}
	}

	// 2. Calling activate_skill unlocks sample.do_work and sets ActiveSkillPrompt.
	res, err := manager.Execute(ctx, Command{
		Action: "activate_skill",
		Input:  map[string]interface{}{"skill_name": "SamplePlugin"},
	})
	if err != nil {
		t.Fatalf("execute activate_skill: %v", err)
	}
	if !strings.Contains(res.Observation, "activated") {
		t.Fatalf("unexpected activate_skill observation: %q", res.Observation)
	}
	if !strings.Contains(manager.ActiveSkillPrompt(), "Sample Skill Instructions") {
		t.Fatalf("expected ActiveSkillPrompt to contain skill instructions, got %q", manager.ActiveSkillPrompt())
	}

	activeToolsAfter := manager.ListActiveTools(ctx)
	unlocked := false
	for _, ti := range activeToolsAfter {
		if ti.Name == "sample.do_work" {
			unlocked = true
			break
		}
	}
	if !unlocked {
		t.Fatalf("expected sample.do_work to be unlocked in ListActiveTools after activate_skill")
	}

	// 3. Clearing active skills gates it again, and DetectActiveSkill unlocks it on intent match.
	manager.ClearActiveSkills()
	for _, ti := range manager.ListActiveTools(ctx) {
		if ti.Name == "sample.do_work" {
			t.Fatalf("expected sample.do_work to be gated again after ClearActiveSkills()")
		}
	}

	matched := manager.DetectActiveSkill(ctx, "请帮我处理一下专属标签任务")
	if matched != "SamplePlugin" {
		t.Fatalf("expected DetectActiveSkill to match SamplePlugin, got %q", matched)
	}
	unlockedByIntent := false
	for _, ti := range manager.ListActiveTools(ctx) {
		if ti.Name == "sample.do_work" {
			unlockedByIntent = true
			break
		}
	}
	if !unlockedByIntent {
		t.Fatalf("expected sample.do_work to be unlocked after DetectActiveSkill match")
	}
}

