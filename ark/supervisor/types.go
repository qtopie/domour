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
}

// Supervisor 统一调度与管理门面契约
type Supervisor interface {
	RegisterProvider(p AgentProvider) error
	GetProvider(name string) (AgentProvider, bool)
	Start(ctx context.Context) error
	Stop() error
	ListProcesses() []*AgentProcess
	GetProcess(id string) (*AgentProcess, error)
	WatchTranscript(ctx context.Context, processID string) (<-chan *TranscriptRecord, error)
	SubmitToolDecision(processID string, decision ToolDecision, reason string) error
	SendInstruction(ctx context.Context, processID string, prompt string) error
	InspectSession(ctx context.Context, explicitProvider string, sessionID string, maxTurns int) (*InspectResult, error)
	Registry() *Registry
	Controller() *Controller
}

