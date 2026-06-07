package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	"github.com/qtopie/domour/ark/bootstrap"
	"github.com/qtopie/domour/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	fmt.Println("=== Starting Domour Simple Integration Example with GitHub Copilot CLI ===")

	// Override DOMOUR_TEST_CLI_ROOTS to prevent session query from scanning large home directories
	os.Setenv("DOMOUR_TEST_CLI_ROOTS", "/tmp/non_existent_domour_cli_roots_for_simple_test")

	// 1. Load and override config to use github-copilot-cli for default and chat entry
	origCfg, err := config.LoadDomourConfig()
	if err != nil {
		origCfg = config.DomourConfig{}
	}

	testCfg := origCfg
	testCfg.DefaultProvider = "github-copilot-cli"
	testCfg.DefaultModel = ""
	if testCfg.Providers == nil {
		testCfg.Providers = make(map[string]config.ProviderConfig)
	}
	testCfg.Providers["github-copilot-cli"] = config.ProviderConfig{
		Enabled: true,
	}
	if testCfg.Entries == nil {
		testCfg.Entries = make(map[string]config.EntryConfig)
	}
	testCfg.Entries["chat"] = config.EntryConfig{
		Provider: "github-copilot-cli",
		Model:    "",
	}

	if err := config.SaveDomourConfig(testCfg); err != nil {
		log.Fatalf("Failed to save test config: %v", err)
	}
	fmt.Println("[Init] Switched default provider and chat entry to 'github-copilot-cli'.")

	// Restore original config on exit
	defer func() {
		fmt.Println("[Cleanup] Restoring original configuration...")
		_ = config.SaveDomourConfig(origCfg)
	}()

	// 2. Start server via bootstrap.Run in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("[Server] Launching Domour server...")
		if err := bootstrap.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("[Server] Error running server: %v", err)
		}
		fmt.Println("[Server] Server stopped.")
	}()

	// Wait 2 seconds for the server to bind and start listening
	time.Sleep(2000 * time.Millisecond)

	// 3. Dial gRPC Server and make a call
	addr := "127.0.0.1:1234"
	fmt.Printf("[Client] Connecting to Domour gRPC server at %s...\n", addr)
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("[Client] Failed to dial server: %v", err)
	}
	defer conn.Close()

	client := chatpb.NewChatServiceClient(conn)

	// Use a 45-second timeout for the real CLI execution
	chatCtx, chatCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer chatCancel()

	fmt.Println("[Client] Sending Chat request with real CLI prompt...")
	
	var stream chatpb.ChatService_ChatClient
	var firstResp *chatpb.ChatResponse
	
	for attempt := 1; attempt <= 15; attempt++ {
		stream, err = client.Chat(chatCtx, &chatpb.ChatRequest{
			SessionId: "simple-real-session",
			Seq:       1,
			Message:   "Say hello in one short sentence.",
		})
		if err == nil {
			resp, recvErr := stream.Recv()
			if recvErr == nil {
				firstResp = resp
				break
			}
			if strings.Contains(recvErr.Error(), "is not ready") {
				fmt.Printf("[Client] Provider not ready yet (attempt %d/15), retrying in 1s...\n", attempt)
				time.Sleep(1 * time.Second)
				continue
			}
			log.Fatalf("[Client] Error receiving from stream: %v", recvErr)
		}
		
		if strings.Contains(err.Error(), "is not ready") {
			fmt.Printf("[Client] Provider not ready yet (attempt %d/15), retrying in 1s...\n", attempt)
			time.Sleep(1 * time.Second)
			continue
		}
		log.Fatalf("[Client] Chat request failed: %v", err)
	}

	fmt.Println("[Client] Receiving stream responses:")
	if firstResp != nil {
		fmt.Printf(">> [Received] Done: %t, Stage: %s, Content: %q\n",
			firstResp.GetDone(),
			firstResp.GetMeta()["stage"],
			firstResp.GetContent(),
		)
	}
	
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("[Client] Error receiving from stream: %v", err)
		}
		fmt.Printf(">> [Received] Done: %t, Stage: %s, Content: %q\n",
			resp.GetDone(),
			resp.GetMeta()["stage"],
			resp.GetContent(),
		)
	}
	fmt.Println("[Client] Stream completed successfully.")

	// 4. Cancel context to cleanly shut down server
	fmt.Println("[Client] Stopping server...")
	cancel()
	wg.Wait()

	fmt.Println("=== Simple Integration Example Finished Successfully ===")
}
