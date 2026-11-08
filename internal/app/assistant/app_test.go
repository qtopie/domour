package assistant_test

import (
	"context"
	"os"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	domourbootstrap "github.com/qtopie/domour/ark/bootstrap"
	domourmodel "github.com/qtopie/domour/ark/cognitor"
	"github.com/qtopie/domour/ark/session"
	publicskill "github.com/qtopie/domour/ark/skill"
	runtimespi "github.com/qtopie/domour/ark/spi/runtime"
	"github.com/qtopie/domour/internal/app/assistant"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/engine"
	db "github.com/qtopie/domour/internal/infra/db"
	localbus "github.com/qtopie/domour/internal/infra/eventbus/local"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
	"google.golang.org/grpc"
)

type customMockStore struct {
	session.SessionStore
}

// SPEC-SPI-006: App 函数选项依赖注入完备性
func TestNewApp_FunctionalOptions(t *testing.T) {
	mockStore := db.NewMemoryStore()
	mockBus := localbus.NewEventBus()
	mockOrch := engine.NewLocalOrchestrator(nil, nil, mockBus)
	mockRouter := runtimespi.NewDefaultDualPathwayRouter()

	app, err := assistant.NewApp(nil,
		assistant.WithStore(mockStore),
		assistant.WithOrchestrator(mockOrch),
		assistant.WithEventBus(mockBus),
		assistant.WithRouter(mockRouter),
	)
	if err != nil {
		t.Fatalf("failed to create NewApp with functional options: %v", err)
	}

	if app == nil {
		t.Fatalf("expected non-nil App")
	}
}

// SPEC-SPI-007: 零外部依赖单机启动与内存存储默认就绪
func TestNewApp_ZeroDependencyDefault(t *testing.T) {
	origStoreEnv := os.Getenv("DOMOUR_STORE")
	os.Setenv("DOMOUR_STORE", "memory")
	defer os.Setenv("DOMOUR_STORE", origStoreEnv)

	app, err := assistant.NewApp(nil)
	if err != nil {
		t.Fatalf("failed to create default standalone app: %v", err)
	}

	if app == nil {
		t.Fatalf("expected non-nil App")
	}

	// Verify that assistant service can be initialized cleanly without DB
	svc := assistant.NewAssistantService(nil, db.NewMemoryStore())
	if svc == nil {
		t.Fatalf("expected non-nil AssistantService")
	}
	if svc.Router() == nil {
		t.Errorf("expected default DualPathwayRouter to be initialized")
	}

	// Verify session read/write in pure memory mode
	ctx := context.Background()
	sess, err := svc.GetSession(ctx, "test-session-zero-dep")
	if err != nil {
		t.Fatalf("unexpected error getting session in memory mode: %v", err)
	}
	if sess.ID != "test-session-zero-dep" {
		t.Errorf("expected session ID 'test-session-zero-dep', got %s", sess.ID)
	}
}

func TestBuildChatSystemPrompt_MergesOverrideAndSkills(t *testing.T) {
	mgr := tool.NewManager(tool.WithCleanupInterval(0))
	defer mgr.Close()
	defer publicskill.Unregister("WeChatHelper")

	publicskill.ReplaceByName(publicskill.Skill{
		Name:         "WeChatHelper",
		Description:  "WeChat multimedia helper",
		Instructions: "Always format WeChat cards cleanly.",
		IntentTags:   []string{"微信卡片"},
	})

	ctx := providerruntime.WithRequestMetadata(context.Background(), providerruntime.RequestMetadata{
		SessionID:          "sess-override-merge",
		SystemPromptExtras: "EXTRA_HOST_RULE_123",
	})

	// 1. With SystemPromptOverride and no intent match -> preserves override + extras + <available_skills>
	p1 := assistant.BuildChatSystemPrompt(ctx, mgr, "normal greeting", nil, "CUSTOM_BASE_PERSONA")
	if !strings.Contains(p1, "CUSTOM_BASE_PERSONA") {
		t.Fatalf("expected CUSTOM_BASE_PERSONA in prompt, got: %s", p1)
	}
	if !strings.Contains(p1, "EXTRA_HOST_RULE_123") {
		t.Fatalf("expected EXTRA_HOST_RULE_123 in prompt, got: %s", p1)
	}
	if !strings.Contains(p1, "<available_skills>") || !strings.Contains(p1, "WeChatHelper") {
		t.Fatalf("expected <available_skills> with WeChatHelper when SystemPromptOverride is set, got: %s", p1)
	}

	// 2. With SystemPromptOverride and intent match -> preserves override + <active_skill>
	p2 := assistant.BuildChatSystemPrompt(ctx, mgr, "请生成一张微信卡片", nil, "CUSTOM_BASE_PERSONA")
	if !strings.Contains(p2, "CUSTOM_BASE_PERSONA") || !strings.Contains(p2, "<active_skill>") || !strings.Contains(p2, "Always format WeChat cards cleanly.") {
		t.Fatalf("expected <active_skill> instructions merged with SystemPromptOverride, got: %s", p2)
	}
}

func TestBoundToolObservation(t *testing.T) {
	short := "normal tool output"
	if got := engine.BoundToolObservation(short, 100); got != short {
		t.Fatalf("expected short observation unchanged, got %q", got)
	}

	longInput := strings.Repeat("A", 500) + strings.Repeat("M", 1000) + strings.Repeat("Z", 500)
	bounded := engine.BoundToolObservation(longInput, 600)
	if !strings.Contains(bounded, "...[truncated ") {
		t.Fatalf("expected truncation marker in bounded observation")
	}
	if !strings.HasPrefix(bounded, strings.Repeat("A", 400)) {
		t.Fatalf("expected head preserved")
	}
	if !strings.HasSuffix(bounded, strings.Repeat("Z", 200)) {
		t.Fatalf("expected tail preserved")
	}
}

type fakeChatModel struct {
	provider string
	model    string
}

func (f *fakeChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	for _, m := range messages {
		if strings.Contains(m.Content, "numbered tactical execution plan") {
			return schema.AssistantMessage("1. Inspect requirements\n2. Design module\n3. Verify output", nil), nil
		}
	}
	return schema.AssistantMessage("Executed with "+f.provider+"/"+f.model, nil), nil
}

func (f *fakeChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := f.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (f *fakeChatModel) BindTools(tools []*schema.ToolInfo) error { return nil }

type fakeCognitor struct {
	lastProvider string
	lastModel    string
}

func (c *fakeCognitor) GetClient(ctx context.Context, entry string) (*proxy.Client, error) {
	return c.GetClientWithOverride(ctx, entry, "default-p", "default-m")
}

func (c *fakeCognitor) GetClientWithOverride(ctx context.Context, entry, provider, model string) (*proxy.Client, error) {
	if provider == "" {
		provider = "default-p"
	}
	if model == "" {
		model = "default-m"
	}
	c.lastProvider = provider
	c.lastModel = model
	return proxy.NewTestClient(provider, model, &fakeChatModel{provider: provider, model: model}), nil
}

type fakeExecutor struct{}

func (fakeExecutor) Execute(ctx context.Context, cmd tool.Command) (tool.Result, error) {
	return tool.Result{}, nil
}
func (fakeExecutor) Veto(ctx context.Context, action string) bool           { return false }
func (fakeExecutor) ListTools(ctx context.Context) ([]tool.ToolInfo, error) { return nil, nil }
func (fakeExecutor) ToolManager() *tool.Manager                             { return nil }

func TestAdaptiveRoutingAndPlannerExecution(t *testing.T) {
	domourmodel.DefaultRegistry().Register(domourmodel.ModelRegistration{
		ID:        "test-planner-reg",
		Provider:  "test-reasoning-provider",
		ModelName: "pro-planner-v1",
		Tags:      []string{"reasoning", "deep", "pro", "cloud"},
	})

	cog := &fakeCognitor{}
	eng := engine.NewEngine(cog, fakeExecutor{})
	svc := assistant.NewAssistantService(eng, db.NewMemoryStore())

	var planningSeen bool
	var replyContent strings.Builder
	err := svc.Chat(context.Background(), shared.MotorChatRequest{
		SessionID: "sess-planner-1",
		Seq:       1,
		Message:   "Please architect and plan a distributed storage refactoring step by step",
	}, "", "", func(ev shared.MotorStreamEvent) error {
		if ev.Thinking != nil && ev.Thinking.Stage == "planning" {
			planningSeen = true
		}
		if ev.Type == 0 && ev.Content != "" {
			replyContent.WriteString(ev.Content)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Chat with planner route failed: %v", err)
	}
	if cog.lastProvider != "test-reasoning-provider" || cog.lastModel != "pro-planner-v1" {
		t.Fatalf("expected router+registry to select test-reasoning-provider/pro-planner-v1, got %s/%s", cog.lastProvider, cog.lastModel)
	}
	if !planningSeen {
		t.Fatalf("expected planner loop to emit a Thinking event with stage='planning'")
	}
	if !strings.Contains(replyContent.String(), "Executed with test-reasoning-provider/pro-planner-v1") {
		t.Fatalf("unexpected final reply content: %q", replyContent.String())
	}
}

func TestBootstrapRegisterAndRunSharedGRPCRebind(t *testing.T) {
	grpcSrv := grpc.NewServer()
	if err := domourbootstrap.RegisterAssistantServices(grpcSrv); err != nil {
		t.Fatalf("RegisterAssistantServices failed: %v", err)
	}

	svc := domourbootstrap.ActiveServiceForServer(grpcSrv)
	if svc == nil {
		t.Fatalf("expected ActiveServiceForServer to return registered AssistantService")
	}

	customStore := db.NewMemoryStore()
	customBus := localbus.NewEventBus()
	customRouter := runtimespi.NewDefaultDualPathwayRouter()

	svc.RebindOptions(
		customStore,
		assistant.WithServiceEventBus(customBus),
		assistant.WithServiceRouter(customRouter),
	)

	if svc.Store() != customStore {
		t.Fatalf("expected Store to be rebound to customStore")
	}
	if svc.EventBus() != customBus {
		t.Fatalf("expected EventBus to be rebound to customBus")
	}
	if svc.Orchestrator() == nil || svc.Orchestrator().EventBus() != customBus {
		t.Fatalf("expected LocalOrchestrator to be rebound to publish on customBus")
	}
}
