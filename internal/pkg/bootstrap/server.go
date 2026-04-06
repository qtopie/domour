package bootstrap

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
	"github.com/qtopie/domour/internal/pkg/agent"
	"github.com/qtopie/domour/internal/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Run starts the Domour server.
// It blocks until the context is canceled or an error occurs.
func Run(ctx context.Context) error {
	store := session.NewMemoryStore()
	defer store.Close()

	service, err := agent.NewServer(store)
	if err != nil {
		return fmt.Errorf("failed to init agent server: %w", err)
	}
	internalMux, err := agent.NewInternalBrainMux()
	if err != nil {
		return fmt.Errorf("failed to init internal brain mux: %w", err)
	}
	address := resolveAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}
	internalAddr := resolveInternalHTTPAddress()
	internalServer := &http.Server{
		Addr:    internalAddr,
		Handler: internalMux,
	}

	grpcServer := grpc.NewServer()
	copilotpb.RegisterCopilotServiceServer(grpcServer, service)
	chatpb.RegisterChatServiceServer(grpcServer, service)
	autopilotpb.RegisterAutopilotServiceServer(grpcServer, service)
	reflection.Register(grpcServer)

	// Graceful shutdown on context cancellation
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
		_ = internalServer.Shutdown(context.Background())
	}()
	go func() {
		fmt.Println("Starting internal brain HTTP on", internalAddr)
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
