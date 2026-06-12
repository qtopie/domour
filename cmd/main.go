package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/qtopie/domour/ark/telemetry"
	acpapi "github.com/qtopie/domour/internal/app/api/acp"
	"github.com/qtopie/domour/internal/brain"
	"github.com/qtopie/domour/internal/infra/llm"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "acp" {
		runACPServer()
		return
	}

	fmt.Println("Welcome to Domour Local CLI!")
	fmt.Println("Usage: domour [acp]")
}

func runACPServer() {
	ctx := context.Background()

	// Initialize Telemetry (Export to stdout for debugging)
	shutdown, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "domour-acp",
		UseStdout:   false, // Set to true if you want trace output in stdout, but it might mess with JSON-RPC
	})
	if err != nil {
		log.Fatalf("Failed to setup telemetry: %v", err)
	}
	defer shutdown(ctx)

	slog.Info("Domour ACP Server starting...", "pid", os.Getpid())
	
	// Initialize Brain/Diencephalon
	node := brain.NewDiencephalonNode()
	node.Start(ctx)

	// Initialize LLM ChatModel for Proxy Mode
	cfg := &llm.Config{
		Provider: "agy-cli",
		Model:    "gemini-3.5-flash",
		ProxyURL: "vproxy", // Use default system vproxy
		Debug:    true,
	}
	chatModel, err := llm.NewChatModel(ctx, cfg)
	if err != nil {
		slog.Error("Failed to initialize chat model", "error", err)
		os.Exit(1)
	}

	server := acpapi.NewServer(node, chatModel)
	transport := acpapi.NewStdioTransport()

	slog.Info("Domour ACP Server running in stdio mode", "provider", cfg.Provider, "model", cfg.Model)
	if err := server.Start(ctx, transport); err != nil {
		slog.Error("ACP Server stopped with error", "error", err)
		os.Exit(1)
	}
}
