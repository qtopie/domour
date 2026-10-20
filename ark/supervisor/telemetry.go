package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// TelemetryServer 配置与实例
type TelemetryServer struct {
	socketPath string
	listener   net.Listener
	httpServer *http.Server
	registry   *Registry
	providers  map[string]AgentProvider
	controller *Controller
	mu         sync.RWMutex
}

// TelemetryConfig 服务配置
type TelemetryConfig struct {
	SocketPath string
	Registry   *Registry
	Controller *Controller
}

// NewTelemetryServer 创建遥测服务端
func NewTelemetryServer(cfg TelemetryConfig) *TelemetryServer {
	if cfg.SocketPath == "" {
		cfg.SocketPath = "/tmp/domour-supervisor.sock"
	}
	s := &TelemetryServer{
		socketPath: cfg.SocketPath,
		registry:   cfg.Registry,
		controller: cfg.Controller,
		providers:  make(map[string]AgentProvider),
	}
	return s
}

// RegisterProvider 注册 Provider
func (s *TelemetryServer) RegisterProvider(p AgentProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[p.Name()] = p
}

// Start 启动 Unix Domain Socket 服务
func (s *TelemetryServer) Start(ctx context.Context) error {
	// 清理遗留 socket 文件
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket %s: %w", s.socketPath, err)
	}
	// 设置权限为仅当前用户可读写
	_ = os.Chmod(s.socketPath, 0600)

	s.listener = listener

	mux := http.NewServeMux()
	mux.HandleFunc("/telemetry", s.handleTelemetry)
	mux.HandleFunc("/hook", s.handleHook)

	s.httpServer = &http.Server{
		Handler: mux,
	}

	go func() {
		_ = s.httpServer.Serve(listener)
	}()

	return nil
}

// Stop 停止服务
func (s *TelemetryServer) Stop() error {
	var err error
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = s.httpServer.Shutdown(ctx)
	}
	if s.socketPath != "" {
		_ = os.Remove(s.socketPath)
	}
	return err
}

func (s *TelemetryServer) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		providerName = "antigravity" // 默认优先适配 Antigravity
	}

	s.mu.RLock()
	provider, exists := s.providers[providerName]
	s.mu.RUnlock()

	if !exists {
		// 尝试任意一个可用 provider
		s.mu.RLock()
		for _, p := range s.providers {
			provider = p
			break
		}
		s.mu.RUnlock()
	}

	if provider == nil {
		http.Error(w, "No provider registered", http.StatusInternalServerError)
		return
	}

	update, err := provider.ParseStatusline(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse statusline: %v", err), http.StatusBadRequest)
		return
	}

	proc := s.registry.UpdateFromStatusline(update, provider.Name())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"id":     proc.ID,
	})
}

func (s *TelemetryServer) handleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	event := r.URL.Query().Get("event")
	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		providerName = "antigravity"
	}

	s.mu.RLock()
	provider := s.providers[providerName]
	s.mu.RUnlock()

	if provider == nil {
		http.Error(w, "Provider not found", http.StatusBadRequest)
		return
	}

	req, err := provider.ParseHookEvent(event, body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse hook event: %v", err), http.StatusBadRequest)
		return
	}

	// 如果有控制器且事件是 PreToolUse，支持挂起并等待审批
	if s.controller != nil && req.Event == "PreToolUse" {
		timeout := 30 * time.Second
		decision, reason := s.controller.WaitForDecision(r.Context(), req.ProcessID, req.ToolName, timeout)

		respBytes, err := provider.FormatHookDecision(req, decision, reason)
		if err != nil {
			http.Error(w, "Failed to format decision", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
		return
	}

	// 默认直接放行
	respBytes, _ := provider.FormatHookDecision(req, DecisionAllow, "")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(respBytes)
}
