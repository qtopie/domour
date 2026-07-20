package assistant

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	autopilotpb "github.com/qtopie/domour/gen/assistant/autopilot"
	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	copilotpb "github.com/qtopie/domour/gen/assistant/copilot"
	internalgrpc "github.com/qtopie/domour/internal/app/api/grpc"
	internalhttp "github.com/qtopie/domour/internal/app/api/http"
	"github.com/qtopie/domour/ark/storage"
	"github.com/qtopie/domour/internal/config"
	"github.com/qtopie/domour/internal/engine"
	localorch "github.com/qtopie/domour/internal/infra/dapr/local"
	localbus "github.com/qtopie/domour/internal/infra/eventbus/local"
	db "github.com/qtopie/domour/internal/infra/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type App struct {
	cfg   config.DomourConfig
	store storage.SessionStore

	grpcAddr string
	httpAddr string
}

type AppOption func(*App)

func WithStore(store storage.SessionStore) AppOption {
	return func(a *App) {
		a.store = store
	}
}

func NewApp(cfg *config.DomourConfig, opts ...AppOption) (*App, error) {
	var actualCfg config.DomourConfig
	if cfg != nil {
		actualCfg = *cfg
	} else {
		loaded, err := config.LoadDomourConfig()
		if err != nil {
			fmt.Printf("Warning: failed to load config: %v. Using defaults.\n", err)
		}
		actualCfg = loaded
	}

	// Register config provider models (deepseek, etc.) into the global model
	// registry so tag-based mode selection (balanced → flash) can find them.
	RegisterConfigProviderModels(&actualCfg)

	store := InitStore(&actualCfg)

	app := &App{
		cfg:   actualCfg,
		store: store,
	}

	for _, opt := range opts {
		opt(app)
	}

	return app, nil
}

func (a *App) GRPCAddr() string { return a.grpcAddr }
func (a *App) HTTPAddr() string { return a.httpAddr }

func (a *App) RegisterGRPC(s *grpc.Server) error {
	// NewReloadableCognitorClient never fails — if the provider is not yet
	// running, it defers initialization until the first use (lazy init).
	cognitorClient := engine.NewReloadableCognitorClient()
	executorClient, err := engine.NewConfiguredExecutorClient()
	if err != nil {
		return fmt.Errorf("failed to init executor client: %w", err)
	}
	eng := engine.NewEngine(cognitorClient, executorClient)

	eb := localbus.NewEventBus()
	orch := localorch.NewLocalOrchestrator(eng, eb)

	appService := NewAssistantService(eng, a.store, eb, orch)

	service, err := internalgrpc.NewServer(appService)
	if err != nil {
		return fmt.Errorf("failed to init agent server: %w", err)
	}

	copilotpb.RegisterCopilotServiceServer(s, service)
	chatpb.RegisterChatServiceServer(s, service)
	autopilotpb.RegisterAutopilotServiceServer(s, service)
	hasReflection := false
	for svcName := range s.GetServiceInfo() {
		if strings.Contains(svcName, "reflection") {
			hasReflection = true
			break
		}
	}
	if !hasReflection {
		reflection.Register(s)
	}

	return nil
}

func (a *App) Run(ctx context.Context) error {
	return a.RunWithNotify(ctx, nil)
}

func (a *App) RunBackground(ctx context.Context) error {
	cognitorClient := engine.NewReloadableCognitorClient()
	executorClient, _ := engine.NewConfiguredExecutorClient()
	eng := engine.NewEngine(cognitorClient, executorClient)

	internalMux, err := internalhttp.NewInternalBrainMux(eng.Cognitor())
	if err != nil {
		return fmt.Errorf("failed to init internal brain mux: %w", err)
	}

	internalAddr := resolveInternalHTTPAddress()
	internalLis, err := net.Listen("tcp", internalAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on internal http %s: %w", internalAddr, err)
	}
	a.httpAddr = internalLis.Addr().String()

	internalServer := &http.Server{
		Handler: internalMux,
	}

	go func() {
		<-ctx.Done()
		_ = internalServer.Shutdown(context.Background())
		_ = internalLis.Close()
		a.store.Close()
	}()

	fmt.Println("Starting internal brain HTTP on", a.httpAddr)
	if err := internalServer.Serve(internalLis); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("internal brain HTTP server error: %w", err)
	}

	return nil
}

func (a *App) RunWithNotify(ctx context.Context, ready chan<- struct{}) error {
	grpcServer := grpc.NewServer()
	if err := a.RegisterGRPC(grpcServer); err != nil {
		return err
	}

	cognitorClient := engine.NewReloadableCognitorClient()
	executorClient, err := engine.NewConfiguredExecutorClient()
	if err != nil {
		return fmt.Errorf("failed to init executor client: %w", err)
	}
	eng := engine.NewEngine(cognitorClient, executorClient)

	internalMux, err := internalhttp.NewInternalBrainMux(eng.Cognitor())
	if err != nil {
		return fmt.Errorf("failed to init internal brain mux: %w", err)
	}

	internalAddr := resolveInternalHTTPAddress()
	internalLis, err := net.Listen("tcp", internalAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on internal http %s: %w", internalAddr, err)
	}
	a.httpAddr = internalLis.Addr().String()

	internalServer := &http.Server{
		Handler: internalMux,
	}

	address := resolveAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		internalLis.Close()
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}
	a.grpcAddr = lis.Addr().String()

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
		_ = internalServer.Shutdown(context.Background())
		_ = internalLis.Close()
		a.store.Close()
	}()

	go func() {
		fmt.Println("Starting internal brain HTTP on", a.httpAddr)
		if err := internalServer.Serve(internalLis); err != nil && err != http.ErrServerClosed {
			fmt.Printf("internal brain HTTP server exited: %v\n", err)
			grpcServer.GracefulStop()
		}
	}()

	if ready != nil {
		close(ready)
	}

	fmt.Println("Starting process on", a.grpcAddr)
	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve gRPC server: %w", err)
	}
	return nil
}

func InitStore(cfg *config.DomourConfig) storage.SessionStore {
	if cfg == nil {
		loaded, _ := config.LoadDomourConfig()
		cfg = &loaded
	}
	if os.Getenv("DOMOUR_USE_SURREAL") == "true" {
		fmt.Println("[Bootstrap] Initializing SurrealDB Session Store with Dapr Discovery...")
		surrealDB, err := db.NewSurrealDB(db.Config{
			Address:     os.Getenv("DOMOUR_SURREAL_ADDR"),
			User:        os.Getenv("DOMOUR_SURREAL_USER"),
			Pass:        os.Getenv("DOMOUR_SURREAL_PASS"),
			Namespace:   "domour",
			Database:    "agent",
			DaprAddress: cfg.DaprHTTPAddress(),
		})
		if err != nil {
			fmt.Printf("Error: failed to connect to SurrealDB: %v. Falling back to BadgerDB.\n", err)
		} else {
			return db.NewSurrealStore(surrealDB)
		}
	}
	// Default: BadgerDB — survives restarts.
	badgerStore, err := db.NewBadgerStore("")
	if err != nil {
		fmt.Printf("Error: failed to open BadgerDB store: %v. Falling back to memory store.\n", err)
		return db.NewMemoryStore()
	}
	fmt.Println("[Bootstrap] Initialized BadgerDB Session Store (survives restarts)")
	return badgerStore
}

func resolveAddress() string {
	if address := strings.TrimSpace(os.Getenv("DOMOUR_ADDRESS")); address != "" {
		return address
	}
	return "127.0.0.1:1234"
}

func resolveInternalHTTPAddress() string {
	if address := strings.TrimSpace(os.Getenv("DOMOUR_INTERNAL_HTTP_ADDRESS")); address != "" {
		return address
	}
	return "127.0.0.1:18080"
}
