package tests_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	// Locate real agy binary
	realAgy, err := exec.LookPath("agy")
	if err != nil {
		t.Fatalf("failed to find real agy command: %v", err)
	}

	// Locate newly compiled vproxy binary from sibling workspace
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	vproxyBin := filepath.Join(filepath.Dir(filepath.Dir(cwd)), "vproxy", "bin", "vproxy")
	if _, err := os.Stat(vproxyBin); err != nil {
		t.Fatalf("vproxy binary not found at %s: %v. Please run go build in vproxy first.", vproxyBin, err)
	}

	// Create symlink or copy of agy named agy-test-bin to bypass global vproxy daemon matching rules
	tmpBin := t.TempDir()
	testBinPath := filepath.Join(tmpBin, "agy-test-bin")

	// Try creating symlink, fallback to copy if it fails
	if err := os.Symlink(realAgy, testBinPath); err != nil {
		input, err := os.Open(realAgy)
		if err != nil {
			t.Fatalf("failed to open real agy: %v", err)
		}
		defer input.Close()
		output, err := os.OpenFile(testBinPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			t.Fatalf("failed to create agy-test-bin: %v", err)
		}
		defer output.Close()
		if _, err := io.Copy(output, input); err != nil {
			t.Fatalf("failed to copy agy to agy-test-bin: %v", err)
		}
	}

	// Symlink newly compiled vproxy to tmpBin/vproxy
	testVProxyPath := filepath.Join(tmpBin, "vproxy")
	if err := os.Symlink(vproxyBin, testVProxyPath); err != nil {
		input, err := os.Open(vproxyBin)
		if err != nil {
			t.Fatalf("failed to open compiled vproxy: %v", err)
		}
		defer input.Close()
		output, err := os.OpenFile(testVProxyPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			t.Fatalf("failed to create test vproxy: %v", err)
		}
		defer output.Close()
		if _, err := io.Copy(output, input); err != nil {
			t.Fatalf("failed to copy vproxy: %v", err)
		}
	}

	// Set PATH to prepend the tmpBin dir
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpBin+string(os.PathListSeparator)+origPath)
	defer os.Setenv("PATH", origPath)

	// Set VP_BYPASS_DAEMON_CHECK env var to bypass background daemon detection in vproxy
	origBypass := os.Getenv("VP_BYPASS_DAEMON_CHECK")
	os.Setenv("VP_BYPASS_DAEMON_CHECK", "1")
	defer func() {
		if origBypass == "" {
			os.Unsetenv("VP_BYPASS_DAEMON_CHECK")
		} else {
			os.Setenv("VP_BYPASS_DAEMON_CHECK", origBypass)
		}
	}()

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
