package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

// DetectClientType 根据日志路径或环境探测客户端形态 ("cli" | "ide")
func DetectClientType(transcriptPath string) string {
	normalized := strings.ReplaceAll(transcriptPath, "\\", "/")
	if strings.Contains(normalized, "/antigravity-cli/") {
		return "cli"
	}
	if strings.Contains(normalized, "/antigravity-ide/") || strings.Contains(normalized, "/.gemini/antigravity/") {
		return "ide"
	}
	return "cli"
}

// FindAgentAPI 自动探测系统中的 AgentAPI 脚本或语言服务二进制
// 返回可执行路径、是否需要附加 "agentapi" 子命令，以及是否发现成功
func FindAgentAPI() (exePath string, isSubcommand bool, ok bool) {
	if envExe := os.Getenv("ANTIGRAVITY_AGENTAPI_EXE"); envExe != "" {
		if _, err := os.Stat(envExe); err == nil {
			isSubcmd := !strings.HasSuffix(filepath.Base(envExe), "agentapi")
			return envExe, isSubcmd, true
		}
	}

	// 1. 检查 PATH 中的 agy（agy CLI 原生支持完整的 agentapi 子命令体系）
	if agyPath, err := exec.LookPath("agy"); err == nil {
		return agyPath, true, true
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = os.Getenv("HOME")
		if homeDir == "" {
			homeDir = os.Getenv("USERPROFILE")
		}
	}

	// 1. 检查 IDE 自带的 ~/.gemini/antigravity/bin/agentapi 包装脚本
	if homeDir != "" {
		scriptPath := filepath.Join(homeDir, ".gemini", "antigravity", "bin", "agentapi")
		if _, err := os.Stat(scriptPath); err == nil {
			return scriptPath, false, true
		}
	}

	// 2. 检查 Linux /opt/Antigravity/resources/bin/language_server
	linuxLS := "/opt/Antigravity/resources/bin/language_server"
	if _, err := os.Stat(linuxLS); err == nil {
		return linuxLS, true, true
	}

	// 3. 检查 Windows %LOCALAPPDATA%\Programs\antigravity\resources\bin\language_server.exe
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		winLS := filepath.Join(localAppData, "Programs", "antigravity", "resources", "bin", "language_server.exe")
		if _, err := os.Stat(winLS); err == nil {
			return winLS, true, true
		}
	} else if homeDir != "" {
		winLS := filepath.Join(homeDir, "AppData", "Local", "Programs", "antigravity", "resources", "bin", "language_server.exe")
		if _, err := os.Stat(winLS); err == nil {
			return winLS, true, true
		}
	}

	return "", false, false
}

// FindIDEExecutable 自动探测 Antigravity IDE 桌面程序安装路径
func FindIDEExecutable() (string, bool) {
	// 1. Linux
	linuxIDE := "/opt/Antigravity/antigravity"
	if _, err := os.Stat(linuxIDE); err == nil {
		return linuxIDE, true
	}

	// 2. Windows
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		winIDE := filepath.Join(localAppData, "Programs", "antigravity", "Antigravity.exe")
		if _, err := os.Stat(winIDE); err == nil {
			return winIDE, true
		}
	}
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		homeDir = os.Getenv("USERPROFILE")
	}
	if homeDir != "" {
		winIDE := filepath.Join(homeDir, "AppData", "Local", "Programs", "antigravity", "Antigravity.exe")
		if _, err := os.Stat(winIDE); err == nil {
			return winIDE, true
		}
	}

	return "", false
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
	if homeDir == "" {
		homeDir = os.Getenv("USERPROFILE")
	}

	candidateDirs := []string{
		filepath.Join(homeDir, ".gemini", "antigravity-cli", "brain"),
		filepath.Join(homeDir, ".gemini", "antigravity", "brain"),
		filepath.Join(homeDir, ".gemini", "antigravity-ide", "brain"),
	}

	for _, brainDir := range candidateDirs {
		directPath := filepath.Join(brainDir, sessionID, ".system_generated", "logs", "transcript.jsonl")
		if _, err := os.Stat(directPath); err == nil {
			return directPath, true
		}

		// 支持前缀匹配
		entries, err := os.ReadDir(brainDir)
		if err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), sessionID) {
					path := filepath.Join(brainDir, e.Name(), ".system_generated", "logs", "transcript.jsonl")
					if _, err := os.Stat(path); err == nil {
						return path, true
					}
				}
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

// DiscoverSessions 自动扫描当前系统归属 Antigravity 的活跃实例
func (p *AntigravityProvider) DiscoverSessions() ([]*supervisor.AgentProcess, error) {
	infos, err := DiscoverRunningSessions()
	if err != nil {
		return nil, err
	}
	res := make([]*supervisor.AgentProcess, 0, len(infos))
	for _, info := range infos {
		if info != nil && info.Process != nil {
			proc := info.Process
			if proc.ClientType == "" && proc.TranscriptPath != "" {
				proc.ClientType = DetectClientType(proc.TranscriptPath)
			}
			res = append(res, proc)
		}
	}
	return res, nil
}

// SpawnSession 拉起全新的任务会话
func (p *AntigravityProvider) SpawnSession(ctx context.Context, req supervisor.SpawnRequest) (*supervisor.SpawnResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}

	// 阶梯 1: 优先尝试活跃语言服务 AgentAPI new-conversation (若未强制 PreferCLI)
	if !req.PreferCLI {
		client := NewAgentAPIClient()
		if client.IsAvailable() {
			if newConv, err := client.NewConversation(ctx, req.Prompt, req.Model, req.Title); err == nil && newConv != nil {
				return &supervisor.SpawnResult{
					SessionID: newConv.ConversationID,
					Mode:      "agentapi_spawn",
					Provider:  ProviderName,
				}, nil
			}
		}
	}

	// 阶梯 2: 尝试以本地原生 CLI (agy) 异步后台拉起 (可根据配置由 vproxy wrapper 纳管，支持 cgroup eBPF 拦截)
	if !req.PreferIDE {
		agyPath, err := exec.LookPath("agy")
		if err != nil && req.VProxyBin != "" {
			agyPath = "agy"
		}
		if agyPath != "" {
			args := []string{
				"-p", req.Prompt,
				"--dangerously-skip-permissions",
			}
			if req.Workspace != "" {
				args = append(args, "--add-dir", req.Workspace)
			}
			if req.Model != "" {
				args = append(args, "--model", req.Model)
			}

			cmd, _ := supervisor.WrapCommandWithVProxy(ctx, agyPath, args, supervisor.LaunchOptions{
				Workspace: req.Workspace,
				UseVProxy: req.UseVProxy,
				VProxyBin: req.VProxyBin,
			})
			if err := cmd.Start(); err == nil {
				pid := 0
				if cmd.Process != nil {
					pid = cmd.Process.Pid
				}

				return &supervisor.SpawnResult{
					PID:      pid,
					Mode:     "cli_spawn",
					Provider: ProviderName,
					Args:     args,
				}, nil
			}
		}
	}

	// 阶梯 3: 降级为唤起 Antigravity IDE 桌面程序
	if idePath, ok := FindIDEExecutable(); ok {
		var args []string
		if req.Workspace != "" {
			args = append(args, req.Workspace)
		}

		cmd := exec.Command(idePath, args...)
		if err := cmd.Start(); err == nil {
			pid := 0
			if cmd.Process != nil {
				pid = cmd.Process.Pid
			}
			return &supervisor.SpawnResult{
				PID:      pid,
				Mode:     "ide_spawn",
				Provider: ProviderName,
				Args:     args,
			}, nil
		}
	}

	return nil, fmt.Errorf("failed to spawn antigravity session: neither agy CLI nor IDE executable available")
}

// InstructSession 向会话下发后续指令
func (p *AntigravityProvider) InstructSession(ctx context.Context, req supervisor.InstructRequest) (*supervisor.InstructResult, error) {
	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id cannot be empty")
	}
	if strings.TrimSpace(req.Instruction) == "" {
		return nil, fmt.Errorf("instruction cannot be empty")
	}

	// 阶梯 1: 尝试通过 AgentAPI send-message 直接向语言服务注入指令
	client := NewAgentAPIClient()
	if client.IsAvailable() {
		if _, err := client.SendMessage(ctx, req.SessionID, req.Instruction, req.Title); err == nil {
			return &supervisor.InstructResult{
				SessionID:   req.SessionID,
				Status:      "sent",
				Mode:        "agentapi_injected",
				Instruction: req.Instruction,
			}, nil
		}
	}

	// 阶梯 2: 若本地存在 agy CLI，拉起原生 CLI 续接会话
	if agyPath, err := exec.LookPath("agy"); err == nil {
		resumeArgs, err := p.BuildResumeArgs(req.SessionID, req.Instruction, supervisor.LaunchOptions{
			DangerouslySkipPermissions: true,
		})
		if err == nil {
			cmd := exec.Command(agyPath, resumeArgs...)
			if err := cmd.Start(); err == nil {
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
		}
	}

	// 阶梯 3: 降级为唤起 Antigravity IDE 桌面程序
	if idePath, ok := FindIDEExecutable(); ok {
		cmd := exec.Command(idePath)
		if err := cmd.Start(); err == nil {
			pid := 0
			if cmd.Process != nil {
				pid = cmd.Process.Pid
			}
			return &supervisor.InstructResult{
				SessionID:   req.SessionID,
				Status:      "resumed",
				Mode:        "ide_spawn",
				Instruction: req.Instruction,
				PID:         pid,
				Exe:         idePath,
			}, nil
		}
	}

	return nil, fmt.Errorf("failed to instruct session %s: neither agentapi, agy CLI nor IDE available", req.SessionID)
}
