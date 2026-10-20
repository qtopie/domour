package generic

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/qtopie/domour/ark/supervisor"
)

const ProviderName = "generic"

// GenericProvider 提供平台中立的基础 AgentProvider 实现
type GenericProvider struct{}

// NewProvider 创建 GenericProvider 实例
func NewProvider() *GenericProvider {
	return &GenericProvider{}
}

func (p *GenericProvider) Name() string {
	return ProviderName
}

type genericStatusline struct {
	ID             string  `json:"id"`
	PID            int     `json:"pid"`
	Workspace      string  `json:"workspace"`
	State          string  `json:"state"`
	ToolPending    bool    `json:"tool_pending"`
	CurrentTool    string  `json:"current_tool"`
	TranscriptPath string  `json:"transcript_path"`
	ContextPercent float64 `json:"context_percent"`
	QuotaRemaining int     `json:"quota_remaining"`
}

func (p *GenericProvider) ParseStatusline(payload []byte) (*supervisor.StatuslineUpdate, error) {
	var raw genericStatusline
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal generic statusline: %w", err)
	}

	if raw.ID == "" {
		return nil, fmt.Errorf("missing id in generic statusline")
	}

	return &supervisor.StatuslineUpdate{
		ID:             raw.ID,
		PID:            raw.PID,
		Workspace:      raw.Workspace,
		AgentState:     raw.State,
		ToolPending:    raw.ToolPending,
		CurrentTool:    raw.CurrentTool,
		TranscriptPath: raw.TranscriptPath,
		ContextPercent: raw.ContextPercent,
		QuotaRemaining: raw.QuotaRemaining,
	}, nil
}

func (p *GenericProvider) ParseHookEvent(event string, payload []byte) (*supervisor.HookRequest, error) {
	var req supervisor.HookRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("failed to parse generic hook: %w", err)
	}
	req.Event = event
	req.RawPayload = payload
	return &req, nil
}

func (p *GenericProvider) FormatHookDecision(req *supervisor.HookRequest, decision supervisor.ToolDecision, reason string) ([]byte, error) {
	resp := map[string]interface{}{
		"decision": string(decision),
		"reason":   reason,
	}
	return json.Marshal(resp)
}

func (p *GenericProvider) ParseTranscriptLine(line []byte) (*supervisor.TranscriptRecord, error) {
	if len(line) == 0 {
		return nil, nil
	}
	var record supervisor.TranscriptRecord
	if err := json.Unmarshal(line, &record); err != nil {
		// 回退为纯文本记录
		return &supervisor.TranscriptRecord{
			Content:   string(line),
			Timestamp: time.Now(),
			RawLine:   line,
		}, nil
	}
	record.RawLine = line
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}
	return &record, nil
}

func (p *GenericProvider) FormatInstruction(prompt string) ([]byte, error) {
	msg := map[string]interface{}{
		"role":    "user",
		"content": prompt,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (p *GenericProvider) BuildHeadlessArgs(opts supervisor.LaunchOptions) ([]string, error) {
	args := make([]string, 0)
	if len(opts.ExtraArgs) > 0 {
		args = append(args, opts.ExtraArgs...)
	}
	return args, nil
}

func (p *GenericProvider) BuildResumeArgs(sessionID string, prompt string, opts supervisor.LaunchOptions) ([]string, error) {
	args := []string{"--resume", sessionID, "--prompt", prompt}
	if len(opts.ExtraArgs) > 0 {
		args = append(args, opts.ExtraArgs...)
	}
	return args, nil
}

// MatchSession 检查 sessionID 是否匹配通用 provider
func (p *GenericProvider) MatchSession(sessionID string) (string, bool) {
	return "", false
}

// ReadRecentTurns 读取该会话最近的对话轮次
func (p *GenericProvider) ReadRecentTurns(sessionID string, maxTurns int) ([]*supervisor.TranscriptRecord, error) {
	return nil, fmt.Errorf("read recent turns not implemented for generic provider")
}

