package assistant_test

import (
	"context"
	"os"
	"testing"

	"github.com/qtopie/domour/ark/session"
	runtimespi "github.com/qtopie/domour/ark/spi/runtime"
	"github.com/qtopie/domour/internal/app/assistant"
	"github.com/qtopie/domour/internal/engine"
	db "github.com/qtopie/domour/internal/infra/db"
	localbus "github.com/qtopie/domour/internal/infra/eventbus/local"
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
