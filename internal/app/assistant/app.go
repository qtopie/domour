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
	"github.com/qtopie/domour/internal/bionic/session"
	"github.com/qtopie/domour/internal/config"
	"github.com/qtopie/domour/internal/engine"
	db "github.com/qtopie/domour/internal/infra/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// App is the assembled server application. It owns the configuration, session store
// and manages the lifecycle of the servers.
type App struct {
	cfg   config.DomourConfig
	store session.Store
}

// NewApp constructs an App with the given config.
func NewApp(cfg *config.DomourConfig) (*App, error) {
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

	store := InitStore(&actualCfg)

	return &App{
		cfg:   actualCfg,
		store: store,
	}, nil
}

// Run starts the core Engine, gRPC server, and the internal brain HTTP server.
// It blocks until the context is canceled or an error occurs.
func (a *App) Run(ctx context.Context) error {
	// 1. Initialize Engine
	cognitorClient, err := engine.NewReloadableCognitorClient()
	if err != nil {
		return fmt.Errorf("failed to init cognitor client: %w", err)
	}
	executorClient, err := engine.NewConfiguredExecutorClient()
	if err != nil {
		return fmt.Errorf("failed to init executor client: %w", err)
	}
	eng := engine.NewEngine(cognitorClient, executorClient)

	// 2. Initialize AssistantService
	appService := NewAssistantService(eng, a.store)

	// 3. Initialize API gRPC server handler
	service, err := internalgrpc.NewServer(appService)
	if err != nil {
		return fmt.Errorf("failed to init agent server: %w", err)
	}

	// 4. Initialize API internal brain HTTP mux
	internalMux, err := internalhttp.NewInternalBrainMux(eng.Cognitor())
	if err != nil {
		return fmt.Errorf("failed to init internal brain mux: %w", err)
	}

	// 5. Register gRPC service implementations
	grpcServer := grpc.NewServer()
	copilotpb.RegisterCopilotServiceServer(grpcServer, service)
	chatpb.RegisterChatServiceServer(grpcServer, service)
	autopilotpb.RegisterAutopilotServiceServer(grpcServer, service)
	reflection.Register(grpcServer)

	internalAddr := resolveInternalHTTPAddress()
	internalServer := &http.Server{
		Addr:    internalAddr,
		Handler: internalMux,
	}

	address := resolveAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
		_ = internalServer.Shutdown(context.Background())
		a.store.Close()
	}()

	go func() {
		fmt.Println("Starting internal brain HTTP on", internalServer.Addr)
		if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("internal brain HTTP server exited: %v\n", err)
			grpcServer.GracefulStop()
		}
	}()

	fmt.Println("Starting process on", address)
	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve gRPC server: %w", err)
	}
	return nil
}

func InitStore(cfg *config.DomourConfig) session.Store {
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
			fmt.Printf("Error: failed to connect to SurrealDB: %v. Falling back to memory store.\n", err)
			return db.NewMemoryStore()
		}
		return db.NewSurrealStore(surrealDB)
	}
	return db.NewMemoryStore()
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
