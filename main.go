package main

import (
	"fmt"
	"log"
	"net"

	"github.com/qtopie/homa/gen/assistant"
	cfg "github.com/qtopie/homa/internal/app/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	// Initialize the PluginManager
	pluginManager := NewPluginManager("/opt/homa/plugins")

	// Create the CopilotServiceServerImpl
	copilotService := NewCopilotServiceServerImpl(pluginManager)

	// Start the gRPC server
	address := cfg.GetAppConfig().GetString("app.address")
	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", address, err)
	}

	grpcServer := grpc.NewServer()
	assistant.RegisterCopilotServiceServer(grpcServer, copilotService)
	reflection.Register(grpcServer)

	fmt.Println("Starting process on", address)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC server: %v", err)
	}
}
