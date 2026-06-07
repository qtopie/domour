package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

type ToolKind string

const (
	ToolKindInternal ToolKind = "internal"
	ToolKindCLI      ToolKind = "cli"
	ToolKindGRPC     ToolKind = "grpc"
	ToolKindMCP      ToolKind = "mcp"

	defaultIdleTTL       = 5 * time.Minute
	defaultCleanupPeriod = 30 * time.Second
)

type ToolInfo struct {
	Name        string            `json:"name"`
	Kind        ToolKind          `json:"kind"`
	Description string            `json:"description,omitempty"`
	Loaded      bool              `json:"loaded"`
	Meta        map[string]string `json:"meta,omitempty"`
}

type ToolRuntime interface {
	Invoke(ctx context.Context, command Command) (Result, error)
	Close(ctx context.Context) error
}

type ToolLoader func(ctx context.Context) (ToolRuntime, error)

type ToolSpec struct {
	Name        string
	Kind        ToolKind
	Description string
	IdleTTL     time.Duration
	Meta        map[string]string
	Load        ToolLoader
}

type toolState struct {
	spec     ToolSpec
	runtime  ToolRuntime
	lastUsed time.Time
	inFlight int
	loading  bool
	cond     *sync.Cond
}

type Manager struct {
	mu              sync.Mutex
	tools           map[string]*toolState
	skills          map[string]*skillState
	cleanupInterval time.Duration
	cancel          context.CancelFunc
}

type ManagerOption func(*Manager)

func WithCleanupInterval(interval time.Duration) ManagerOption {
	return func(m *Manager) {
		m.cleanupInterval = interval
	}
}

func NewManager(opts ...ManagerOption) *Manager {
	manager := &Manager{
		tools:           make(map[string]*toolState),
		skills:          make(map[string]*skillState),
		cleanupInterval: defaultCleanupPeriod,
	}
	for _, opt := range opts {
		opt(manager)
	}

	if manager.cleanupInterval > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		manager.cancel = cancel
		go manager.runJanitor(ctx)
	}
	return manager
}

func NewDefaultManager() (*Manager, error) {
	manager := NewManager()
	if err := manager.Register(NewInternalTool("render_d2", "Render D2 diagrams locally with the built-in renderer", NewD2Renderer().Act)); err != nil {
		manager.Close()
		return nil, err
	}
	if err := manager.Register(NewShellTool("shell.exec", "Execute a local shell command through the motor-managed CLI runtime")); err != nil {
		manager.Close()
		return nil, err
	}
	if err := manager.Register(NewSearchGrepTool()); err != nil {
		manager.Close()
		return nil, err
	}
	if err := manager.Register(NewFileEditLinesTool()); err != nil {
		manager.Close()
		return nil, err
	}
	if err := manager.Register(NewFileReplaceTool()); err != nil {
		manager.Close()
		return nil, err
	}
	if err := manager.LoadDefaultSkillSources(); err != nil {
		manager.Close()
		return nil, err
	}
	return manager, nil
}

func (m *Manager) Register(spec ToolSpec) error {
	if err := validateToolSpec(spec); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tools[spec.Name]; exists {
		return fmt.Errorf("tool %q already registered", spec.Name)
	}

	state := &toolState{spec: normalizeToolSpec(spec)}
	state.cond = sync.NewCond(&m.mu)
	m.tools[state.spec.Name] = state
	return nil
}

func (m *Manager) List() []ToolInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	tools := make([]ToolInfo, 0, len(m.tools))
	for _, state := range m.tools {
		tools = append(tools, ToolInfo{
			Name:        state.spec.Name,
			Kind:        state.spec.Kind,
			Description: state.spec.Description,
			Loaded:      state.runtime != nil,
			Meta:        cloneMeta(state.spec.Meta),
		})
	}
	return tools
}

func (m *Manager) Execute(ctx context.Context, command Command) (Result, error) {
	if strings.TrimSpace(command.Action) == "" {
		return Result{}, fmt.Errorf("motor command action is required")
	}

	state, runtime, err := m.acquire(ctx, command.Action)
	if err != nil {
		return Result{}, err
	}
	defer m.release(state)

	result, err := runtime.Invoke(ctx, command)
	if err != nil {
		return Result{}, err
	}
	if result.CommandID == "" {
		result.CommandID = firstNonEmpty(strings.TrimSpace(command.ID), command.Action)
	}
	if result.Meta == nil {
		result.Meta = map[string]string{}
	}
	if result.Meta["tool"] == "" {
		result.Meta["tool"] = command.Action
	}
	if result.Meta["kind"] == "" {
		result.Meta["kind"] = string(state.spec.Kind)
	}
	return result, nil
}

func (m *Manager) Unload(name string) error {
	runtime, err := m.detachRuntime(name, false)
	if err != nil {
		return err
	}
	if runtime == nil {
		return nil
	}
	return runtime.Close(context.Background())
}

func (m *Manager) UnloadIdle(ctx context.Context) error {
	runtimes := m.collectExpired(time.Now())
	for _, runtime := range runtimes {
		if err := runtime.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Close() error {
	if m.cancel != nil {
		m.cancel()
	}

	m.mu.Lock()
	runtimes := make([]ToolRuntime, 0, len(m.tools))
	for _, state := range m.tools {
		if state.runtime != nil {
			runtimes = append(runtimes, state.runtime)
			state.runtime = nil
			state.lastUsed = time.Time{}
		}
	}
	for _, state := range m.skills {
		state.loaded = nil
		state.lastUsed = time.Time{}
	}
	m.mu.Unlock()

	var firstErr error
	for _, runtime := range runtimes {
		if err := runtime.Close(context.Background()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) acquire(ctx context.Context, name string) (*toolState, ToolRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.tools[name]
	if !ok {
		return nil, nil, fmt.Errorf("tool %q is not registered", name)
	}

	for state.loading {
		state.cond.Wait()
	}

	if state.runtime != nil {
		state.inFlight++
		state.lastUsed = time.Now()
		return state, state.runtime, nil
	}

	state.loading = true
	load := state.spec.Load
	m.mu.Unlock()
	runtime, err := load(ctx)
	m.mu.Lock()
	state.loading = false
	state.cond.Broadcast()
	if err != nil {
		return nil, nil, err
	}

	state.runtime = runtime
	state.inFlight++
	state.lastUsed = time.Now()
	return state, runtime, nil
}

func (m *Manager) release(state *toolState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if state.inFlight > 0 {
		state.inFlight--
	}
	state.lastUsed = time.Now()
}

func (m *Manager) detachRuntime(name string, force bool) (ToolRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q is not registered", name)
	}
	if state.runtime == nil {
		return nil, nil
	}
	if !force && state.inFlight > 0 {
		return nil, nil
	}
	runtime := state.runtime
	state.runtime = nil
	state.lastUsed = time.Time{}
	return runtime, nil
}

func (m *Manager) collectExpired(now time.Time) []ToolRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()

	var runtimes []ToolRuntime
	for _, state := range m.tools {
		if state.runtime == nil || state.inFlight > 0 {
			continue
		}
		if now.Sub(state.lastUsed) < state.spec.IdleTTL {
			continue
		}
		runtimes = append(runtimes, state.runtime)
		state.runtime = nil
		state.lastUsed = time.Time{}
	}
	return runtimes
}

func (m *Manager) runJanitor(ctx context.Context) {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.UnloadIdle(context.Background())
			m.UnloadIdleSkills()
		}
	}
}

func validateToolSpec(spec ToolSpec) error {
	if strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("tool name is required")
	}
	if spec.Load == nil {
		return fmt.Errorf("tool %q loader is required", spec.Name)
	}
	switch spec.Kind {
	case ToolKindInternal, ToolKindCLI, ToolKindGRPC, ToolKindMCP:
	default:
		return fmt.Errorf("tool %q has unsupported kind %q", spec.Name, spec.Kind)
	}
	return nil
}

func normalizeToolSpec(spec ToolSpec) ToolSpec {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Description = strings.TrimSpace(spec.Description)
	if spec.IdleTTL <= 0 {
		spec.IdleTTL = defaultIdleTTL
	}
	spec.Meta = cloneMeta(spec.Meta)
	return spec
}

func cloneMeta(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

type InternalToolHandler func(ctx context.Context, command Command) (Result, error)

func NewInternalTool(name, description string, handler InternalToolHandler) ToolSpec {
	return ToolSpec{
		Name:        name,
		Kind:        ToolKindInternal,
		Description: description,
		Load: func(context.Context) (ToolRuntime, error) {
			return internalToolRuntime{handler: handler}, nil
		},
	}
}

type internalToolRuntime struct {
	handler InternalToolHandler
}

func (r internalToolRuntime) Invoke(ctx context.Context, command Command) (Result, error) {
	return r.handler(ctx, command)
}

func (r internalToolRuntime) Close(context.Context) error {
	return nil
}

type CLIInvocation struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
	Stdin   string
}

type CLIInvocationBuilder func(ctx context.Context, command Command) (CLIInvocation, error)

func NewCLITool(name, description string, builder CLIInvocationBuilder) ToolSpec {
	return ToolSpec{
		Name:        name,
		Kind:        ToolKindCLI,
		Description: description,
		Load: func(context.Context) (ToolRuntime, error) {
			return cliToolRuntime{builder: builder}, nil
		},
	}
}

func NewShellTool(name, description string) ToolSpec {
	return NewCLITool(name, description, func(ctx context.Context, command Command) (CLIInvocation, error) {
		raw, _ := command.Input["command"].(string)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return CLIInvocation{}, fmt.Errorf("%s requires input.command", name)
		}

		dir, _ := command.Input["dir"].(string)
		dir = strings.TrimSpace(dir)
		if dir == "" {
			dir = strings.TrimSpace(command.Meta["workspace"])
		}

		if runtime.GOOS == "windows" {
			return CLIInvocation{
				Command: "cmd",
				Args:    []string{"/C", raw},
				Dir:     dir,
			}, nil
		}
		return CLIInvocation{
			Command: "sh",
			Args:    []string{"-lc", raw},
			Dir:     dir,
		}, nil
	})
}

type cliToolRuntime struct {
	builder CLIInvocationBuilder
}

func (r cliToolRuntime) Invoke(ctx context.Context, command Command) (Result, error) {
	invocation, err := r.builder(ctx, command)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(invocation.Command) == "" {
		return Result{}, fmt.Errorf("cli tool requires a command")
	}

	cmd := exec.CommandContext(ctx, invocation.Command, invocation.Args...)
	cmd.Dir = strings.TrimSpace(invocation.Dir)
	if len(invocation.Env) > 0 {
		cmd.Env = append(os.Environ(), invocation.Env...)
	}
	if invocation.Stdin != "" {
		cmd.Stdin = strings.NewReader(invocation.Stdin)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Result{}, fmt.Errorf("cli tool failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return Result{
		CommandID:   firstNonEmpty(strings.TrimSpace(command.ID), command.Action),
		Observation: string(output),
		Done:        true,
		Meta: map[string]string{
			"command": invocation.Command,
		},
	}, nil
}

func (r cliToolRuntime) Close(context.Context) error {
	return nil
}

type GRPCToolClient interface {
	Invoke(ctx context.Context, command Command) (Result, error)
	Close(ctx context.Context) error
}

type GRPCToolClientFactory func(ctx context.Context) (GRPCToolClient, error)

func NewGRPCTool(name, description string, factory GRPCToolClientFactory) ToolSpec {
	return ToolSpec{
		Name:        name,
		Kind:        ToolKindGRPC,
		Description: description,
		Load: func(ctx context.Context) (ToolRuntime, error) {
			client, err := factory(ctx)
			if err != nil {
				return nil, err
			}
			return grpcToolRuntime{client: client}, nil
		},
	}
}

type grpcToolRuntime struct {
	client GRPCToolClient
}

func (r grpcToolRuntime) Invoke(ctx context.Context, command Command) (Result, error) {
	return r.client.Invoke(ctx, command)
}

func (r grpcToolRuntime) Close(ctx context.Context) error {
	return r.client.Close(ctx)
}

type MCPCallResult struct {
	Content string
	Meta    map[string]string
}

type MCPToolClient interface {
	CallTool(ctx context.Context, name string, args map[string]interface{}) (MCPCallResult, error)
	Close(ctx context.Context) error
}

type MCPToolClientFactory func(ctx context.Context) (MCPToolClient, error)

func NewMCPTool(name, remoteName, description string, factory MCPToolClientFactory) ToolSpec {
	remoteName = firstNonEmpty(strings.TrimSpace(remoteName), strings.TrimSpace(name))
	return ToolSpec{
		Name:        name,
		Kind:        ToolKindMCP,
		Description: description,
		Load: func(ctx context.Context) (ToolRuntime, error) {
			client, err := factory(ctx)
			if err != nil {
				return nil, err
			}
			return &mcpToolRuntime{
				client:     client,
				remoteName: remoteName,
			}, nil
		},
	}
}

type mcpToolRuntime struct {
	client     MCPToolClient
	remoteName string
}

func (r *mcpToolRuntime) Invoke(ctx context.Context, command Command) (Result, error) {
	args := make(map[string]interface{}, len(command.Input))
	for key, value := range command.Input {
		args[key] = value
	}
	reply, err := r.client.CallTool(ctx, r.remoteName, args)
	if err != nil {
		return Result{}, err
	}
	return Result{
		CommandID:   firstNonEmpty(strings.TrimSpace(command.ID), command.Action),
		Observation: reply.Content,
		Done:        true,
		Meta:        cloneMeta(reply.Meta),
	}, nil
}

func (r *mcpToolRuntime) Close(ctx context.Context) error {
	return r.client.Close(ctx)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
