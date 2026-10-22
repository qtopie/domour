package supervisor

import (
	"context"
	"time"
)

// ProcessStatus 表示 Agent 实例的生命周期状态
type ProcessStatus string

const (
	StatusUnknown              ProcessStatus = "unknown"
	StatusInitializing         ProcessStatus = "initializing"
	StatusThinking             ProcessStatus = "thinking"
	StatusWorking              ProcessStatus = "working"
	StatusToolUse              ProcessStatus = "tool_use"
	StatusAwaitingConfirmation ProcessStatus = "awaiting_confirmation"
	StatusIdle                 ProcessStatus = "idle"
	StatusTerminated           ProcessStatus = "terminated"
)

// ToolDecision 表示对工具执行审批的决策结果
type ToolDecision string

const (
	DecisionAllow   ToolDecision = "allow"
	DecisionDeny    ToolDecision = "deny"
	DecisionAskUser ToolDecision = "ask_user"
)

// ToolCallInfo 结构化工具调用信息
type ToolCallInfo struct {
	ToolName   string                 `json:"tool_name"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
	Arguments  map[string]interface{} `json:"arguments,omitempty"`
	Summary    string                 `json:"summary,omitempty"`
}

// AgentProcess 统一的受管/被发现 Agent 实例实体
type AgentProcess struct {
	ID             string        `json:"id"`
	PID            int           `json:"pid"`
	Provider       string        `json:"provider"`
	ClientType     string        `json:"client_type,omitempty"` // "cli" | "ide"
	Workspace      string        `json:"workspace"`
	Status         ProcessStatus `json:"status"`
	ToolPending    bool          `json:"tool_pending"`
	CurrentTool    string        `json:"current_tool,omitempty"`
	TranscriptPath string        `json:"transcript_path,omitempty"`
	ContextPercent float64       `json:"context_percent"`
	QuotaRemaining int           `json:"quota_remaining"`
	LastHeartbeat  time.Time     `json:"last_heartbeat"`
}

// TranscriptRecord 统一的单步执行记录结构
type TranscriptRecord struct {
	StepIndex int                    `json:"step_index"`
	Type      string                 `json:"type"`
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Thinking  string                 `json:"thinking,omitempty"`
	ToolCalls []ToolCallInfo         `json:"tool_calls,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	RawLine   []byte                 `json:"raw_line,omitempty"`
}

// StatuslineUpdate 从状态栏/心跳提取的增量状态
type StatuslineUpdate struct {
	ID             string
	PID            int
	Workspace      string
	AgentState     string
	ToolPending    bool
	CurrentTool    string
	TranscriptPath string
	ContextPercent float64
	QuotaRemaining int
}

// HookRequest 生命周期 Hook 触发的请求
type HookRequest struct {
	Event      string                 `json:"event"`
	ProcessID  string                 `json:"process_id"`
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
	Arguments  map[string]interface{} `json:"arguments,omitempty"`
	RawPayload []byte                 `json:"raw_payload,omitempty"`
}

// LaunchOptions 启动 Agent 进程的参数配置
type LaunchOptions struct {
	Command                     string            `json:"command,omitempty"`
	Workspace                   string            `json:"workspace"`
	SessionID                   string            `json:"session_id,omitempty"`
	DangerouslySkipPermissions  bool              `json:"dangerously_skip_permissions"`
	Env                         map[string]string `json:"env,omitempty"`
	ExtraArgs                   []string          `json:"extra_args,omitempty"`
	UseVProxy                   bool              `json:"use_vproxy,omitempty"`
	VProxyBin                   string            `json:"vproxy_bin,omitempty"`
}

// SpawnRequest 统一拉起新任务的请求结构
type SpawnRequest struct {
	Provider  string `json:"provider,omitempty"`
	Prompt    string `json:"prompt"`
	Workspace string `json:"workspace,omitempty"`
	Model     string `json:"model,omitempty"`
	Title     string `json:"title,omitempty"`
	PreferIDE bool   `json:"prefer_ide,omitempty"`
	PreferCLI bool   `json:"prefer_cli,omitempty"`
	UseVProxy bool   `json:"use_vproxy,omitempty"`
	VProxyBin string `json:"vproxy_bin,omitempty"`
}

// SpawnResult 统一拉起新任务的结果结构
type SpawnResult struct {
	SessionID string   `json:"session_id"`
	PID       int      `json:"pid,omitempty"`
	Mode      string   `json:"mode"` // "agentapi_spawn", "cli_spawn", "ide_spawn"
	Provider  string   `json:"provider"`
	Args      []string `json:"args,omitempty"`
}

// InstructRequest 统一向会话下发后续指令的请求结构
type InstructRequest struct {
	SessionID   string `json:"session_id"`
	Instruction string `json:"instruction"`
	Provider    string `json:"provider,omitempty"`
	Title       string `json:"title,omitempty"`
}

// InstructResult 统一指令下发结果结构
type InstructResult struct {
	SessionID   string `json:"session_id"`
	Status      string `json:"status"` // "sent", "resumed"
	Mode        string `json:"mode"`   // "stream_pipe", "agentapi_injected", "cli_spawn", "ide_spawn"
	Instruction string `json:"instruction"`
	PID         int    `json:"pid,omitempty"`
	Exe         string `json:"exe,omitempty"`
}

// Supervisor 统一调度与管理门面契约
type Supervisor interface {
	RegisterProvider(p AgentProvider) error
	GetProvider(name string) (AgentProvider, bool)
	Start(ctx context.Context) error
	Stop() error
	ListProcesses() []*AgentProcess
	GetProcess(id string) (*AgentProcess, error)
	DiscoverSessions(ctx context.Context) ([]*AgentProcess, error)
	InspectSession(ctx context.Context, explicitProvider string, sessionID string, maxTurns int) (*InspectResult, error)
	SpawnSession(ctx context.Context, req SpawnRequest) (*SpawnResult, error)
	InstructSession(ctx context.Context, req InstructRequest) (*InstructResult, error)
	WatchTranscript(ctx context.Context, processID string) (<-chan *TranscriptRecord, error)
	SubmitToolDecision(processID string, decision ToolDecision, reason string) error
	SendInstruction(ctx context.Context, processID string, prompt string) error
	Registry() *Registry
	Controller() *Controller
}

