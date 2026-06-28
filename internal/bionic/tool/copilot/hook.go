package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	hookFileName     = "domour-control.json"
	hookVersion      = 1
	hookTimeoutSec   = 5
	serverShutdownTO = 3 * time.Second
)

// hookConfig is the JSON structure written to ~/.copilot/hooks/domour-control.json.
type hookConfig struct {
	Version int                  `json:"version"`
	Hooks   map[string][]hookEntry `json:"hooks"`
}

type hookEntry struct {
	Type       string `json:"type"`
	URL        string `json:"url"`
	TimeoutSec int    `json:"timeoutSec"`
}

// HookServer starts an HTTP server on a random port that handles Copilot CLI
// hook callbacks (preToolUse, postToolUse). It installs the hook config file
// into ~/.copilot/hooks/ automatically.
type HookServer struct {
	veto     *VetoEngine
	server   *http.Server
	listener net.Listener
	addr     string

	mu       sync.Mutex
	events   []HookEvent // ring-buffer of recent hook events for observability
}

// HookEvent records a single preToolUse or postToolUse call for audit/debugging.
type HookEvent struct {
	Kind      string    `json:"kind"`     // "preToolUse" | "postToolUse"
	ToolName  string    `json:"toolName"`
	SessionID string    `json:"sessionId"`
	Decision  string    `json:"decision,omitempty"` // only for preToolUse
	At        time.Time `json:"at"`
}

// NewHookServer creates (but does not start) a HookServer with the given VetoEngine.
func NewHookServer(veto *VetoEngine) *HookServer {
	return &HookServer{veto: veto}
}

// Start binds a random TCP port, begins serving hook callbacks,
// and installs the Copilot hook config file.
func (s *HookServer) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("hook server: listen on random port: %w", err)
	}
	s.listener = ln
	s.addr = ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/hooks/preToolUse", s.handlePreToolUse)
	mux.HandleFunc("/hooks/postToolUse", s.handlePostToolUse)
	mux.HandleFunc("/hooks/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	s.server = &http.Server{
		Handler:     mux,
		ReadTimeout: 10 * time.Second,
	}

	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("Hook server error", "error", err)
		}
	}()

	slog.Info("Domour hook server started", "addr", s.addr)

	if err := s.installHookConfig(); err != nil {
		_ = s.server.Shutdown(context.Background())
		return fmt.Errorf("hook server: install hook config: %w", err)
	}

	return nil
}

// Addr returns the "host:port" the server is listening on.
func (s *HookServer) Addr() string { return s.addr }

// Port returns only the port number as a string.
func (s *HookServer) Port() string {
	_, port, _ := net.SplitHostPort(s.addr)
	return port
}

// RecentEvents returns a snapshot of recent hook events (for observability).
func (s *HookServer) RecentEvents() []HookEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]HookEvent, len(s.events))
	copy(cp, s.events)
	return cp
}

// Stop gracefully shuts down the HTTP server and removes the hook config file.
func (s *HookServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), serverShutdownTO)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		return err
	}
	return s.removeHookConfig()
}

// ServePreToolUse is the exported handler for tests and direct HTTP mux registration.
func (s *HookServer) ServePreToolUse(w http.ResponseWriter, r *http.Request) {
	s.handlePreToolUse(w, r)
}

// ServePostToolUse is the exported handler for tests and direct HTTP mux registration.
func (s *HookServer) ServePostToolUse(w http.ResponseWriter, r *http.Request) {
	s.handlePostToolUse(w, r)
}

// handlePreToolUse implements the preToolUse hook HTTP endpoint.
// Copilot CLI POSTs the tool call payload here and expects a VetoDecision JSON back.
func (s *HookServer) handlePreToolUse(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var payload HookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Warn("preToolUse: failed to parse payload, defaulting to allow", "error", err)
		writeDecision(w, VetoDecision{PermissionDecision: "allow"})
		return
	}

	decision := s.veto.Evaluate(payload)

	s.recordEvent(HookEvent{
		Kind:      "preToolUse",
		ToolName:  payload.EffectiveToolName(),
		SessionID: payload.SessionID,
		Decision:  decision.PermissionDecision,
		At:        time.Now(),
	})

	slog.Info("preToolUse hook",
		"tool", payload.EffectiveToolName(),
		"decision", decision.PermissionDecision,
		"session", payload.SessionID,
	)

	writeDecision(w, decision)
}

// handlePostToolUse records the tool result for observability but does not modify it.
func (s *HookServer) handlePostToolUse(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload HookPayload
	if err := json.Unmarshal(body, &payload); err == nil {
		s.recordEvent(HookEvent{
			Kind:      "postToolUse",
			ToolName:  payload.EffectiveToolName(),
			SessionID: payload.SessionID,
			At:        time.Now(),
		})
	}

	// Return empty JSON to keep original result unchanged.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (s *HookServer) recordEvent(ev HookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	const maxEvents = 200
	if len(s.events) >= maxEvents {
		s.events = s.events[1:]
	}
	s.events = append(s.events, ev)
}

func writeDecision(w http.ResponseWriter, d VetoDecision) {
	b, _ := json.Marshal(d)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// installHookConfig writes ~/.copilot/hooks/domour-control.json pointing at this server.
func (s *HookServer) installHookConfig() error {
	dir, err := copilotHooksDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create hooks dir %s: %w", dir, err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%s", s.Port())

	cfg := hookConfig{
		Version: hookVersion,
		Hooks: map[string][]hookEntry{
			"preToolUse": {
				{
					Type:       "http",
					URL:        baseURL + "/hooks/preToolUse",
					TimeoutSec: hookTimeoutSec,
				},
			},
			"postToolUse": {
				{
					Type:       "http",
					URL:        baseURL + "/hooks/postToolUse",
					TimeoutSec: hookTimeoutSec,
				},
			},
		},
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal hook config: %w", err)
	}

	path := filepath.Join(dir, hookFileName)
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("write hook config %s: %w", path, err)
	}

	slog.Info("Installed Copilot hook config", "path", path)
	return nil
}

func (s *HookServer) removeHookConfig() error {
	dir, err := copilotHooksDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, hookFileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove hook config %s: %w", path, err)
	}
	slog.Info("Removed Copilot hook config", "path", path)
	return nil
}

// copilotHooksDir returns the ~/.copilot/hooks directory,
// respecting the COPILOT_HOME environment variable override.
func copilotHooksDir() (string, error) {
	base := os.Getenv("COPILOT_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home dir: %w", err)
		}
		base = filepath.Join(home, ".copilot")
	}
	return filepath.Join(base, "hooks"), nil
}
