package antigravity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
)


const ProviderName = "antigravity"

// AntigravityProvider 实现针对 Antigravity CLI (agy) 的 AgentProvider 接口
type AntigravityProvider struct{}

// NewProvider 创建 AntigravityProvider 实例
func NewProvider() *AntigravityProvider {
	return &AntigravityProvider{}
}

func (p *AntigravityProvider) Name() string {
	return ProviderName
}

type statuslinePayload struct {
	ConversationID          string `json:"conversation_id"`
	SessionID               string `json:"session_id"`
	PID                     int    `json:"pid"`
	CWD                     string `json:"cwd"`
	Workspace               string `json:"workspace"`
	AgentState              string `json:"agent_state"`
	ToolConfirmationPending bool   `json:"tool_confirmation_pending"`
	CurrentTool             string `json:"current_tool"`
	TranscriptPath          string `json:"transcript_path"`
	ContextWindow           *struct {
		PercentUsed float64 `json:"percent_used"`
	} `json:"context_window"`
	Quota *struct {
		RemainingQuota int `json:"remaining_quota"`
	} `json:"quota"`
}

// ParseStatusline 反序列化 agy 的状态栏 payload
func (p *AntigravityProvider) ParseStatusline(payload []byte) (*supervisor.StatuslineUpdate, error) {
	var raw statuslinePayload
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agy statusline payload: %w", err)
	}

	id := raw.ConversationID
	if id == "" {
		id = raw.SessionID
	}
	if id == "" {
		return nil, fmt.Errorf("missing conversation_id or session_id in payload")
	}

	ws := raw.Workspace
	if ws == "" {
		ws = raw.CWD
	}

	update := &supervisor.StatuslineUpdate{
		ID:             id,
		PID:            raw.PID,
		Workspace:      ws,
		AgentState:     raw.AgentState,
		ToolPending:    raw.ToolConfirmationPending,
		CurrentTool:    raw.CurrentTool,
		TranscriptPath: raw.TranscriptPath,
	}

	if raw.ContextWindow != nil {
		update.ContextPercent = raw.ContextWindow.PercentUsed
	}
	if raw.Quota != nil {
		update.QuotaRemaining = raw.Quota.RemainingQuota
	}

	return update, nil
}

type hookPayload struct {
	Event      string                 `json:"event"`
	ToolName   string                 `json:"tool_name"`
	ToolCallID string                 `json:"tool_call_id"`
	Arguments  map[string]interface{} `json:"arguments"`
	Context    struct {
		ConversationID string `json:"conversation_id"`
		SessionID      string `json:"session_id"`
		StepIndex      int    `json:"step_index"`
	} `json:"context"`
}

// ParseHookEvent 反序列化 agy 的 hook payload
func (p *AntigravityProvider) ParseHookEvent(event string, payload []byte) (*supervisor.HookRequest, error) {
	var raw hookPayload
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse agy hook event %s: %w", event, err)
	}

	id := raw.Context.ConversationID
	if id == "" {
		id = raw.Context.SessionID
	}

	req := &supervisor.HookRequest{
		Event:      event,
		ProcessID:  id,
		ToolName:   raw.ToolName,
		ToolCallID: raw.ToolCallID,
		Arguments:  raw.Arguments,
		RawPayload: payload,
	}
	return req, nil
}

// FormatHookDecision 格式化 agy hook 放行响应
func (p *AntigravityProvider) FormatHookDecision(req *supervisor.HookRequest, decision supervisor.ToolDecision, reason string) ([]byte, error) {
	resp := make(map[string]interface{})
	switch decision {
	case supervisor.DecisionAllow:
		resp["decision"] = "allow"
	case supervisor.DecisionDeny:
		resp["decision"] = "deny"
		if reason != "" {
			resp["reason"] = reason
		} else {
			resp["reason"] = "Denied by Domour Supervisor"
		}
	default:
		resp["decision"] = "ask_user"
	}

	return json.Marshal(resp)
}

type transcriptLine struct {
	StepIndex int    `json:"step_index"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	Content   string `json:"content"`
	Thinking  string `json:"thinking"`
	ToolCalls []struct {
		ToolName  string                 `json:"tool_name"`
		Summary   string                 `json:"summary"`
		Arguments map[string]interface{} `json:"arguments"`
	} `json:"tool_calls"`
}

// ParseTranscriptLine 解析单行 transcript.jsonl
func (p *AntigravityProvider) ParseTranscriptLine(line []byte) (*supervisor.TranscriptRecord, error) {
	if len(line) == 0 {
		return nil, nil
	}

	var raw transcriptLine
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, fmt.Errorf("invalid agy transcript line: %w", err)
	}

	role := "assistant"
	if raw.Type == "USER_INPUT" || raw.Source == "USER_EXPLICIT" {
		role = "user"
	} else if raw.Source == "SYSTEM" {
		role = "system"
	}

	var parsedTime time.Time
	if raw.CreatedAt != "" {
		parsedTime, _ = time.Parse(time.RFC3339, raw.CreatedAt)
	}

	toolCalls := make([]supervisor.ToolCallInfo, 0, len(raw.ToolCalls))
	for _, tc := range raw.ToolCalls {
		toolCalls = append(toolCalls, supervisor.ToolCallInfo{
			ToolName:  tc.ToolName,
			Arguments: tc.Arguments,
			Summary:   tc.Summary,
		})
	}

	record := &supervisor.TranscriptRecord{
		StepIndex: raw.StepIndex,
		Type:      raw.Type,
		Role:      role,
		Content:   raw.Content,
		Thinking:  raw.Thinking,
		ToolCalls: toolCalls,
		Timestamp: parsedTime,
		RawLine:   line,
	}

	return record, nil
}

// FormatInstruction 格式化发送给 agy stream-json 的指令消息
func (p *AntigravityProvider) FormatInstruction(prompt string) ([]byte, error) {
	msg := map[string]interface{}{
		"event": "user",
		"message": map[string]interface{}{
			"content": prompt,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// BuildHeadlessArgs 构造 agy stream-json 启动参数
func (p *AntigravityProvider) BuildHeadlessArgs(opts supervisor.LaunchOptions) ([]string, error) {
	args := []string{
		"-p", "",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	}

	if opts.Workspace != "" {
		args = append(args, "--add-dir", opts.Workspace)
	}

	if opts.SessionID != "" {
		args = append(args, "--conversation", opts.SessionID)
	}

	if opts.DangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	if len(opts.ExtraArgs) > 0 {
		args = append(args, opts.ExtraArgs...)
	}

	return args, nil
}

// BuildResumeArgs 构造会话续接命令参数
func (p *AntigravityProvider) BuildResumeArgs(sessionID string, prompt string, opts supervisor.LaunchOptions) ([]string, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("sessionID cannot be empty for resume")
	}

	args := []string{
		"--conversation", sessionID,
		"-p", prompt,
	}

	if opts.Workspace != "" {
		args = append(args, "--add-dir", opts.Workspace)
	}

	if opts.DangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	if len(opts.ExtraArgs) > 0 {
		args = append(args, opts.ExtraArgs...)
	}

	return args, nil
}

// MatchSession 检查 sessionID 是否匹配 Antigravity 的 brain 会话目录
func (p *AntigravityProvider) MatchSession(sessionID string) (string, bool) {
	if sessionID == "" {
		return "", false
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = os.Getenv("HOME")
	}

	brainDir := filepath.Join(homeDir, ".gemini", "antigravity-cli", "brain")
	directPath := filepath.Join(brainDir, sessionID, ".system_generated", "logs", "transcript.jsonl")
	if _, err := os.Stat(directPath); err == nil {
		return directPath, true
	}

	// 支持前缀匹配
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		return "", false
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), sessionID) {
			path := filepath.Join(brainDir, e.Name(), ".system_generated", "logs", "transcript.jsonl")
			if _, err := os.Stat(path); err == nil {
				return path, true
			}
		}
	}

	return "", false
}

// ReadRecentTurns 读取该会话最近的对话轮次
func (p *AntigravityProvider) ReadRecentTurns(sessionID string, maxTurns int) ([]*supervisor.TranscriptRecord, error) {
	transcriptPath, ok := p.MatchSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found in antigravity brain directory", sessionID)
	}

	return ReadSessionRecentTurns(transcriptPath, maxTurns)
}
