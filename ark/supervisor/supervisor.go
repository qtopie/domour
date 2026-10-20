package supervisor

import (
	"context"
	"fmt"
	"sync"
)

type defaultSupervisor struct {
	registry   *Registry
	telemetry  *TelemetryServer
	watcher    *TranscriptWatcher
	controller *Controller
	providers  map[string]AgentProvider
	mu         sync.RWMutex
}

// Config 创建 Supervisor 的可选项
type Config struct {
	SocketPath string
}

// New 创建默认 Supervisor 实例
func New(cfg Config) (Supervisor, error) {
	watcher, err := NewTranscriptWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize transcript watcher: %w", err)
	}

	reg := NewRegistry()
	ctrl := NewController(reg)

	tel := NewTelemetryServer(TelemetryConfig{
		SocketPath: cfg.SocketPath,
		Registry:   reg,
		Controller: ctrl,
	})

	s := &defaultSupervisor{
		registry:   reg,
		telemetry:  tel,
		watcher:    watcher,
		controller: ctrl,
		providers:  make(map[string]AgentProvider),
	}

	return s, nil
}

func (s *defaultSupervisor) RegisterProvider(p AgentProvider) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.providers[p.Name()] = p
	s.telemetry.RegisterProvider(p)
	return nil
}

func (s *defaultSupervisor) GetProvider(name string) (AgentProvider, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.providers[name]
	return p, ok
}

func (s *defaultSupervisor) Start(ctx context.Context) error {
	return s.telemetry.Start(ctx)
}

func (s *defaultSupervisor) Stop() error {
	_ = s.telemetry.Stop()
	_ = s.watcher.Close()
	return nil
}

func (s *defaultSupervisor) ListProcesses() []*AgentProcess {
	return s.registry.List()
}

func (s *defaultSupervisor) GetProcess(id string) (*AgentProcess, error) {
	proc, ok := s.registry.Get(id)
	if !ok {
		return nil, fmt.Errorf("process with id %s not found", id)
	}
	return proc, nil
}

func (s *defaultSupervisor) WatchTranscript(ctx context.Context, processID string) (<-chan *TranscriptRecord, error) {
	proc, ok := s.registry.Get(processID)
	if !ok {
		return nil, fmt.Errorf("process %s not found", processID)
	}
	if proc.TranscriptPath == "" {
		return nil, fmt.Errorf("process %s has no transcript_path recorded", processID)
	}

	s.mu.RLock()
	provider, exists := s.providers[proc.Provider]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("provider %s not registered", proc.Provider)
	}

	ch := make(chan *TranscriptRecord, 100)
	if err := s.watcher.WatchFile(ctx, proc.TranscriptPath, provider, ch); err != nil {
		close(ch)
		return nil, err
	}

	return ch, nil
}

func (s *defaultSupervisor) SubmitToolDecision(processID string, decision ToolDecision, reason string) error {
	return s.controller.SubmitToolDecision(processID, decision, reason)
}

func (s *defaultSupervisor) SendInstruction(ctx context.Context, processID string, prompt string) error {
	return s.controller.SendInstruction(ctx, processID, prompt)
}

// Controller 返回底层控制器引用（便于更细粒度的控制）
func (s *defaultSupervisor) Controller() *Controller {
	return s.controller
}

// Registry 返回底层注册表引用
func (s *defaultSupervisor) Registry() *Registry {
	return s.registry
}
