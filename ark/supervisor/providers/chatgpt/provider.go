package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/qtopie/domour/ark/supervisor"
)

const ProviderName = "chatgpt"

// Options 用于配置 ChatGPTProvider
type Options struct {
	DBPath    string
	CodexPath string
}

// ChatGPTProvider 统一实现针对 ChatGPT 与 OpenAI Codex CLI 的 AgentProvider 接口
type ChatGPTProvider struct {
	opts Options
}

// NewProvider 创建默认的 ChatGPTProvider
func NewProvider() *ChatGPTProvider {
	return NewProviderWithOptions(Options{})
}

// NewProviderWithOptions 使用自定义选项创建 ChatGPTProvider
func NewProviderWithOptions(opts Options) *ChatGPTProvider {
	if opts.DBPath == "" {
		opts.DBPath = DefaultDBPath()
	}
	if opts.CodexPath == "" {
		if path, err := exec.LookPath("codex"); err == nil {
			opts.CodexPath = path
		}
	}
	return &ChatGPTProvider{opts: opts}
}

func (p *ChatGPTProvider) Name() string {
	return ProviderName
}

// ParseStatusline 反序列化状态栏数据
func (p *ChatGPTProvider) ParseStatusline(payload []byte) (*supervisor.StatuslineUpdate, error) {
	var raw struct {
		ID          string  `json:"id"`
		PID         int     `json:"pid"`
		Workspace   string  `json:"workspace"`
		AgentState  string  `json:"agent_state"`
		ToolPending bool    `json:"tool_pending"`
		CurrentTool string  `json:"current_tool"`
		TokensUsed  int     `json:"tokens_used"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal chatgpt statusline payload: %w", err)
	}

	return &supervisor.StatuslineUpdate{
		ID:             raw.ID,
		PID:            raw.PID,
		Workspace:      raw.Workspace,
		AgentState:     raw.AgentState,
		ToolPending:    raw.ToolPending,
		CurrentTool:    raw.CurrentTool,
		QuotaRemaining: 100,
	}, nil
}

// ParseHookEvent 反序列化 Hook 事件
func (p *ChatGPTProvider) ParseHookEvent(event string, payload []byte) (*supervisor.HookRequest, error) {
	var raw struct {
		Event      string                 `json:"event"`
		ToolName   string                 `json:"tool_name"`
		ToolCallID string                 `json:"tool_call_id"`
		Arguments  map[string]interface{} `json:"arguments"`
		SessionID  string                 `json:"session_id"`
	}

	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse hook event payload: %w", err)
	}

	req := &supervisor.HookRequest{
		Event:      event,
		ProcessID:  raw.SessionID,
		ToolName:   raw.ToolName,
		ToolCallID: raw.ToolCallID,
		Arguments:  raw.Arguments,
		RawPayload: payload,
	}
	return req, nil
}

// FormatHookDecision 将统一审批结果格式化为适配该 Agent 的响应结构
func (p *ChatGPTProvider) FormatHookDecision(req *supervisor.HookRequest, decision supervisor.ToolDecision, reason string) ([]byte, error) {
	decStr := "allow"
	if decision == supervisor.DecisionDeny {
		decStr = "deny"
	} else if decision == supervisor.DecisionAskUser {
		decStr = "ask"
	}
	return FormatApprovalDecision(decStr, reason)
}

// ParseTranscriptLine 将底层的单行日志（JSONL）反序列化为统一的 TranscriptRecord
func (p *ChatGPTProvider) ParseTranscriptLine(line []byte) (*supervisor.TranscriptRecord, error) {
	return ParseTranscriptLine(line)
}

// FormatInstruction 将要发送给 Agent 的用户指令格式化为对应输入字节
func (p *ChatGPTProvider) FormatInstruction(prompt string) ([]byte, error) {
	return []byte(prompt), nil
}

// BuildHeadlessArgs 构造流式交互（Headless）长连接启动参数
func (p *ChatGPTProvider) BuildHeadlessArgs(opts supervisor.LaunchOptions) ([]string, error) {
	args := []string{"exec", "--json"}

	if opts.Workspace != "" {
		args = append(args, "--cd", opts.Workspace)
	}

	if opts.DangerouslySkipPermissions {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}

	if len(opts.ExtraArgs) > 0 {
		args = append(args, opts.ExtraArgs...)
	}

	return args, nil
}

// BuildResumeArgs 构造会话续接命令行参数
func (p *ChatGPTProvider) BuildResumeArgs(sessionID string, prompt string, opts supervisor.LaunchOptions) ([]string, error) {
	args := []string{"exec", "resume"}
	if sessionID != "" {
		args = append(args, sessionID)
	} else {
		args = append(args, "--last")
	}

	if prompt != "" {
		args = append(args, prompt)
	}

	return args, nil
}

// MatchSession 检查给定的 sessionID 或前缀是否归属当前 Provider
func (p *ChatGPTProvider) MatchSession(sessionID string) (string, bool) {
	if strings.TrimSpace(sessionID) == "" {
		return "", false
	}

	// 1. 从 SQLite 中检索匹配的 thread
	threads, err := QueryThreads(p.opts.DBPath)
	if err == nil {
		for _, thr := range threads {
			if thr.ID == sessionID || strings.HasPrefix(thr.ID, sessionID) {
				return thr.RolloutPath, true
			}
		}
	}

	// 2. 检查会话前缀习惯 (如 "thr_" 或 "chat_")
	if strings.HasPrefix(sessionID, "thr_") || strings.HasPrefix(sessionID, "chat_") {
		return "", true
	}

	return "", false
}

// ReadRecentTurns 读取该会话最近的若干轮对话记录
func (p *ChatGPTProvider) ReadRecentTurns(sessionID string, maxTurns int) ([]*supervisor.TranscriptRecord, error) {
	rolloutPath, ok := p.MatchSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %s not found in chatgpt/codex provider", sessionID)
	}

	if rolloutPath != "" {
		if _, err := os.Stat(rolloutPath); err == nil {
			return ReadTurnsFromRollout(rolloutPath, maxTurns)
		}
	}

	// 若无 rollout 文件，尝试从 threads 数据库中提取首条概要
	threads, err := QueryThreads(p.opts.DBPath)
	if err == nil {
		for _, thr := range threads {
			if thr.ID == sessionID || strings.HasPrefix(thr.ID, sessionID) {
				var recs []*supervisor.TranscriptRecord
				if thr.FirstUserMessage != "" {
					recs = append(recs, &supervisor.TranscriptRecord{
						StepIndex: 1,
						Role:      "user",
						Type:      "user_input",
						Content:   thr.FirstUserMessage,
					})
				}
				if thr.Title != "" {
					recs = append(recs, &supervisor.TranscriptRecord{
						StepIndex: 2,
						Role:      "assistant",
						Type:      "assistant_response",
						Content:   fmt.Sprintf("[Codex/ChatGPT Thread: %s] Model: %s", thr.Title, thr.Model),
					})
				}
				return recs, nil
			}
		}
	}

	return nil, fmt.Errorf("unable to read turns for session %s", sessionID)
}

// DiscoverSessions 自动扫描当前系统归属该 Provider 的活跃实例
func (p *ChatGPTProvider) DiscoverSessions() ([]*supervisor.AgentProcess, error) {
	return DiscoverRunningSessions(p.opts.DBPath)
}

// SpawnSession 拉起全新的任务会话
func (p *ChatGPTProvider) SpawnSession(ctx context.Context, req supervisor.SpawnRequest) (*supervisor.SpawnResult, error) {
	codexBin := p.opts.CodexPath
	if codexBin == "" {
		var err error
		codexBin, err = exec.LookPath("codex")
		if err != nil {
			return nil, fmt.Errorf("codex executable not found in PATH")
		}
	}

	ws := req.Workspace
	if ws == "" {
		ws, _ = os.Getwd()
	}

	args := []string{"exec", "--json", "--cd", ws}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	if req.Prompt != "" {
		args = append(args, req.Prompt)
	}

	cmd := exec.CommandContext(ctx, codexBin, args...)
	cmd.Dir = ws

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to spawn codex/chatgpt session: %w", err)
	}

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	sessionID := fmt.Sprintf("thr_%s", filepath.Base(ws))
	return &supervisor.SpawnResult{
		SessionID: sessionID,
		PID:       pid,
		Mode:      "cli_spawn",
		Provider:  ProviderName,
		Args:      args,
	}, nil
}

// InstructSession 向会话下发后续指令
func (p *ChatGPTProvider) InstructSession(ctx context.Context, req supervisor.InstructRequest) (*supervisor.InstructResult, error) {
	codexBin := p.opts.CodexPath
	if codexBin == "" {
		var err error
		codexBin, err = exec.LookPath("codex")
		if err != nil {
			return nil, fmt.Errorf("codex executable not found in PATH")
		}
	}

	args, err := p.BuildResumeArgs(req.SessionID, req.Instruction, supervisor.LaunchOptions{})
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, codexBin, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to resume session %s: %w", req.SessionID, err)
	}

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	return &supervisor.InstructResult{
		SessionID:   req.SessionID,
		Status:      "resumed",
		Mode:        "cli_spawn",
		Instruction: req.Instruction,
		PID:         pid,
	}, nil
}
