package tests_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qtopie/domour/ark/bootstrap"
	chatpb "github.com/qtopie/domour/gen/assistant/chat"
	"github.com/qtopie/domour/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type testProxyServer struct {
	listener net.Listener
	server   *http.Server
	mu       sync.Mutex
	accessed bool
}

func startTestProxy(t *testing.T) *testProxyServer {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for proxy: %v", err)
	}

	p := &testProxyServer{
		listener: l,
	}

	p.server = &http.Server{
		Handler: p,
	}

	go func() {
		_ = p.server.Serve(l)
	}()

	return p
}

func (p *testProxyServer) Addr() string {
	return "http://" + p.listener.Addr().String()
}

func (p *testProxyServer) Close() {
	_ = p.server.Close()
	_ = p.listener.Close()
}

func (p *testProxyServer) WasAccessed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accessed
}

func (p *testProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

func TestAgyVProxyIntegration(t *testing.T) {
	// 1. Isolate config to a temp dir by overriding HOME env var
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Set debug and test cli roots env vars
	os.Setenv("DOMOUR_DEBUG", "true")
	os.Setenv("DOMOUR_TEST_CLI_ROOTS", "/tmp/non_existent_domour_cli_roots_for_integration_test")

	// 2. Start local proxy
	proxy := startTestProxy(t)
	defer proxy.Close()

	// 3. Write test configuration file
	cfg := config.DomourConfig{
		DefaultProvider: "agy-cli",
		Providers: map[string]config.ProviderConfig{
			"agy-cli": {
				Enabled:    true,
				HTTPSProxy: proxy.Addr(),
			},
		},
		Entries: map[string]config.EntryConfig{
			"chat": {
				Provider: "agy-cli",
			},
		},
	}

	cfgPath, err := config.DomourConfigPath()
	if err != nil {
		t.Fatalf("failed to get config path: %v", err)
	}

	err = config.SaveDomourConfigAt(cfgPath, cfg)
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Make sure we reload the global config cache
	_, err = config.ReloadDomourConfig()
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	// 4. Start Domour server in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := bootstrap.Run(ctx); err != nil && err != context.Canceled {
			t.Errorf("server error: %v", err)
		}
	}()

	// Wait for server to start listening
	time.Sleep(2 * time.Second)

	// 5. Dial server
	conn, err := grpc.Dial("127.0.0.1:1234", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	defer conn.Close()

	client := chatpb.NewChatServiceClient(conn)

	chatCtx, chatCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer chatCancel()

	// 6. Make request with retry loop (waiting for agy health check to complete)
	var stream chatpb.ChatService_ChatClient
	var firstResp *chatpb.ChatResponse

	for attempt := 1; attempt <= 15; attempt++ {
		stream, err = client.Chat(chatCtx, &chatpb.ChatRequest{
			SessionId: "integration-vproxy-session",
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
				t.Logf("Provider not ready (recv attempt %d/15), retrying...", attempt)
				time.Sleep(1 * time.Second)
				continue
			}
			t.Fatalf("error receiving from stream: %v", recvErr)
		}

		if strings.Contains(err.Error(), "is not ready") {
			t.Logf("Provider not ready (chat attempt %d/15), retrying...", attempt)
			time.Sleep(1 * time.Second)
			continue
		}
		t.Fatalf("chat request failed: %v", err)
	}

	// Consume remainder of stream
	if firstResp != nil {
		t.Logf("Received content: %q", firstResp.GetContent())
	}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("error receiving from stream: %v", err)
		}
		t.Logf("Received content: %q", resp.GetContent())
	}

	// 7. Verify proxy was accessed by vproxy
	if !proxy.WasAccessed() {
		t.Error("FAIL: local proxy server was NOT accessed by vproxy wrapper")
	} else {
		t.Log("SUCCESS: local proxy server was accessed by vproxy wrapper")
	}

	// 8. Clean up
	cancel()
	wg.Wait()
}
