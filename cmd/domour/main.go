package main

import (
	"fmt"
	"log"
	"net"

	"github.com/qtopie/domour/gen/assistant"
	cfg "github.com/qtopie/domour/internal/app/config"
	"github.com/qtopie/domour/internal/infra/cache/l1"
	"github.com/qtopie/domour/internal/infra/cache/l2"
	"github.com/qtopie/domour/internal/infra/db"
	"github.com/qtopie/domour/internal/infra/eventbus"
	"github.com/qtopie/domour/internal/pkg/plugin"
	"github.com/qtopie/domour/internal/service/copilot"
	"github.com/qtopie/domour/internal/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	// Load configuration
	appConfig := cfg.GetAppConfig()

	// Initialize L1 Cache (Otter)
	l1Cache, err := l1.NewCache[string, session.Session](
		appConfig.GetInt("cache.l1.capacity"),
		appConfig.GetDuration("cache.l1.ttl"),
	)
	if err != nil {
		log.Fatalf("Failed to initialize L1 cache: %v", err)
	}

	// Initialize L2 Cache (BadgerDB)
	l2Cache, err := l2.NewCache[session.Session](
		appConfig.GetString("cache.l2.dir"),
		appConfig.GetDuration("cache.l2.ttl"),
	)
	if err != nil {
		log.Fatalf("Failed to initialize L2 cache: %v", err)
	}
	defer l2Cache.Close()

	// Initialize SurrealDB
	surrealDB, err := db.NewSurrealDB(db.Config{
		Address:   appConfig.GetString("surrealdb.address"),
		User:      appConfig.GetString("surrealdb.user"),
		Pass:      appConfig.GetString("surrealdb.pass"),
		Namespace: appConfig.GetString("surrealdb.namespace"),
		Database:  appConfig.GetString("surrealdb.database"),
	})
	if err != nil {
		log.Fatalf("Failed to initialize SurrealDB: %v", err)
	}
	defer surrealDB.Close()

	// Initialize NATS EventBus
	eb, err := eventbus.NewEventBus(eventbus.Config{
		URL:           appConfig.GetString("nats.url"),
		StreamName:    appConfig.GetString("nats.stream_name"),
		SubjectPrefix: appConfig.GetString("nats.subject_prefix"),
	})
	if err != nil {
		log.Fatalf("Failed to initialize NATS: %v", err)
	}
	defer eb.Close()

	// Initialize Session Manager
	sessionManager := session.NewManager(l1Cache, l2Cache, surrealDB, eb)
	defer sessionManager.Close()

	// Initialize the PluginManager
	pluginManager := plugin.NewPluginManager("/opt/homa/plugins")

	// Create the CopilotServiceServerImpl
	copilotService := copilot.NewServiceServerImpl(pluginManager, sessionManager)

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
