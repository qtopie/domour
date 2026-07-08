package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
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

// ProxyServer is a local HTTP/HTTPS proxy handler for vproxy validation
type ProxyServer struct {
	listener net.Listener
	server   *http.Server
	mu       sync.Mutex
	accessed bool
}

func StartLocalProxy() (*ProxyServer, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	p := &ProxyServer{
		listener: l,
	}

	p.server = &http.Server{
		Handler: p,
	}

	go func() {
		_ = p.server.Serve(l)
	}()

	return p, nil
}

func (p *ProxyServer) Addr() string {
	return "http://" + p.listener.Addr().String()
}

func (p *ProxyServer) Close() {
	_ = p.server.Close()
	_ = p.listener.Close()
}

func (p *ProxyServer) WasAccessed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accessed
}

func (p *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.accessed = true
	p.mu.Unlock()

	if r.Method == http.MethodConnect {
		destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)

		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
			return
		}

		clientConn, _, err := hijacker.Hijack()
		if err != nil {
			destConn.Close()
			return
		}

		go func() {
			defer destConn.Close()
			defer clientConn.Close()
			_, _ = io.Copy(destConn, clientConn)
		}()
		go func() {
			defer destConn.Close()
			defer clientConn.Close()
			_, _ = io.Copy(clientConn, destConn)
		}()
		return
	}

	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func main() {
	fmt.Println("=== Starting Domour Agy-CLI vproxy Integration Example ===")

	// 1. Enable debug logging to verify vproxy executions
	os.Setenv("DOMOUR_DEBUG", "true")
	os.Setenv("DOMOUR_TEST_CLI_ROOTS", "/tmp/non_existent_domour_cli_roots_for_simple_test")

	// 2. Start local HTTP/HTTPS proxy
	proxyServer, err := StartLocalProxy()
	if err != nil {
		log.Fatalf("Failed to start local proxy: %v", err)
	}
	defer proxyServer.Close()
	fmt.Printf("[Proxy] Local proxy server running at %s\n", proxyServer.Addr())

	// 3. Load and override config to use agy-cli with the local proxy
	origCfg, err := config.LoadDomourConfig()
	if err != nil {
		origCfg = config.DomourConfig{}
	}

	testCfg := origCfg
	testCfg.DefaultProvider = "agy-cli"
	testCfg.DefaultModel = ""
	if testCfg.Providers == nil {
		testCfg.Providers = make(map[string]config.ProviderConfig)
	}
	testCfg.Providers["agy-cli"] = config.ProviderConfig{
		Enabled:    true,
		HTTPSProxy: proxyServer.Addr(),
	}
	if testCfg.Entries == nil {
		testCfg.Entries = make(map[string]config.EntryConfig)
	}
	testCfg.Entries["chat"] = config.EntryConfig{
		Provider: "agy-cli",
		Model:    "",
	}

	if err := config.SaveDomourConfig(testCfg); err != nil {
		log.Fatalf("Failed to save test config: %v", err)
	}
	fmt.Println("[Init] Switched default provider and chat entry to 'agy-cli' with proxy configuration.")

	// Restore original config on exit
	defer func() {
		fmt.Println("[Cleanup] Restoring original configuration...")
		_ = config.SaveDomourConfig(origCfg)
	}()

	// 4. Start server via bootstrap.Run in background
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

	// 5. Dial gRPC Server and make a call
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

	fmt.Println("[Client] Sending Chat request to agy-cli...")
	
	var stream chatpb.ChatService_ChatClient
	var firstResp *chatpb.ChatResponse
	
	for attempt := 1; attempt <= 15; attempt++ {
		stream, err = client.Chat(chatCtx, &chatpb.ChatRequest{
			SessionId: "agy-vproxy-session",
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
		fmt.Printf(">> [Received] chunkSeq=%d maxChecksum=%d, Stage: %s, Content: %q\n",
			firstResp.GetChunkSeq(),
			firstResp.GetMaxSeqChecksum(),
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
		fmt.Printf(">> [Received] chunkSeq=%d maxChecksum=%d, Stage: %s, Content: %q\n",
			resp.GetChunkSeq(),
			resp.GetMaxSeqChecksum(),
			resp.GetMeta()["stage"],
			resp.GetContent(),
		)
	}
	fmt.Println("[Client] Stream completed successfully.")

	// Verify if proxy server was indeed accessed by vproxy
	if proxyServer.WasAccessed() {
		fmt.Println("[Verification] SUCCESS: Local proxy server was successfully accessed by vproxy wrapper!")
	} else {
		log.Fatalf("[Verification] FAILED: Local proxy server was NOT accessed by vproxy wrapper!")
	}

	// 6. Cancel context to cleanly shut down server
	fmt.Println("[Client] Stopping server...")
	cancel()
	wg.Wait()

	fmt.Println("=== Agy-CLI vproxy Integration Example Finished Successfully ===")
}
