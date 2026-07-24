package grpc_test

import (
	"context"
	"sync"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	appgrpc "github.com/qtopie/domour/internal/app/api/grpc"
	"github.com/qtopie/domour/internal/app/assistant"
	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/cognitor/proxy"
	"github.com/qtopie/domour/internal/engine"
	providerruntime "github.com/qtopie/domour/internal/infra/llm/runtime"
	db "github.com/qtopie/domour/internal/infra/db"
	"google.golang.org/grpc"
)

type mockDiencephalonClient struct {
	mu          sync.Mutex
	activeSess  map[string]int
	maxParallel map[string]int
}

func (m *mockDiencephalonClient) Generate(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	metadata := providerruntime.RequestMetadataFromContext(ctx)
	sessionID := metadata.SessionID
	if sessionID == "" {
		sessionID = "default"
	}

	m.mu.Lock()
	if m.activeSess == nil {
		m.activeSess = make(map[string]int)
		m.maxParallel = make(map[string]int)
	}
	m.activeSess[sessionID]++
	if m.activeSess[sessionID] > m.maxParallel[sessionID] {
		m.maxParallel[sessionID] = m.activeSess[sessionID]
	}
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.activeSess[sessionID]--
		m.mu.Unlock()
	}()

	time.Sleep(100 * time.Millisecond)

	return schema.AssistantMessage("Mocked response", nil), nil
}

func (m *mockDiencephalonClient) Stream(ctx context.Context, messages []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *mockDiencephalonClient) BindTools(tools []*schema.ToolInfo) error { return nil }

type mockCognitorClient struct {
	chat *mockDiencephalonClient
}

func (m *mockCognitorClient) GetClient(ctx context.Context, entry string) (*proxy.Client, error) {
	return proxy.NewTestClient("mock", "mock", m.chat), nil
}
func (m *mockCognitorClient) GetClientWithOverride(ctx context.Context, entry string, provider, model string) (*proxy.Client, error) {
	return proxy.NewTestClient(provider, model, m.chat), nil
}

type mockExecutorClient struct{}

func (m *mockExecutorClient) Execute(ctx context.Context, command tool.Command) (tool.Result, error) {
	return tool.Result{}, nil
}
func (m *mockExecutorClient) Veto(ctx context.Context, action string) bool {
	return false
}
func (m *mockExecutorClient) ListTools(ctx context.Context) ([]tool.ToolInfo, error) {
	return nil, nil
}
func (m *mockExecutorClient) ToolManager() *tool.Manager {
	return nil
}

type mockServerStreamingServer struct {
	grpc.ServerStreamingServer[chatpb.ChatResponse]
	ctx  context.Context
	sent []*chatpb.ChatResponse
	mu   sync.Mutex
}

func (m *mockServerStreamingServer) Context() context.Context {
	return m.ctx
}

func (m *mockServerStreamingServer) Send(resp *chatpb.ChatResponse) error {
	m.mu.Lock()
	m.sent = append(m.sent, resp)
	m.mu.Unlock()
	return nil
}

func TestServer_SessionLocking(t *testing.T) {
	store := db.NewMemoryStore()
	chatClient := &mockDiencephalonClient{}
	brain := &mockCognitorClient{chat: chatClient}
	eng := engine.NewEngine(brain, &mockExecutorClient{})
	appService := assistant.NewAssistantService(eng, store)

	srv, err := appgrpc.NewServer(appService)
	if err != nil {
		t.Fatalf("failed to create grpc server: %v", err)
	}

	sessionID := "test-serialized-sess"
	var wg sync.WaitGroup

	// Run two parallel chat requests on the same session
	wg.Add(2)
	runRequest := func(seq int32) {
		defer wg.Done()
		stream := &mockServerStreamingServer{
			ctx: context.Background(),
		}
		req := &chatpb.ChatRequest{
			SessionId: sessionID,
			Seq:       seq,
			Message:   "hello",
		}
		err := srv.Chat(req, stream)
		if err != nil {
			t.Errorf("Chat failed: %v", err)
		}
	}

	go runRequest(1)
	go runRequest(2)

	wg.Wait()

	chatClient.mu.Lock()
	maxParallel := chatClient.maxParallel[sessionID]
	chatClient.mu.Unlock()

	if maxParallel > 1 {
		t.Errorf("Session lock failed: max parallel requests for session %s was %d, expected at most 1", sessionID, maxParallel)
	}
}

