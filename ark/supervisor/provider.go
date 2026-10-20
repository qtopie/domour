package supervisor

// AgentProvider 定义各类 Agent 运行时（CLI、Daemon 或 SDK 封装）的标准适配接口
type AgentProvider interface {
	// Name 返回适配器标识名称，例如 "antigravity", "claude-code", "generic"
	Name() string

	// ParseStatusline 将状态栏或遥测上报的原始字节反序列化为 StatuslineUpdate
	ParseStatusline(payload []byte) (*StatuslineUpdate, error)

	// ParseHookEvent 将生命周期 Hook 触发的事件反序列化为 HookRequest
	ParseHookEvent(event string, payload []byte) (*HookRequest, error)

	// FormatHookDecision 将统一审批结果格式化为适配该 Agent 的响应结构（如 JSON）
	FormatHookDecision(req *HookRequest, decision ToolDecision, reason string) ([]byte, error)

	// ParseTranscriptLine 将底层的单行日志（如 JSONL）反序列化为统一的 TranscriptRecord
	ParseTranscriptLine(line []byte) (*TranscriptRecord, error)

	// FormatInstruction 将要发送给 Agent 的用户指令格式化为对应 Agent 识别的输入字节（如 stream-json）
	FormatInstruction(prompt string) ([]byte, error)

	// BuildHeadlessArgs 构造流式交互（Headless）长连接启动参数
	BuildHeadlessArgs(opts LaunchOptions) ([]string, error)

	// BuildResumeArgs 构造会话续接命令行参数
	BuildResumeArgs(sessionID string, prompt string, opts LaunchOptions) ([]string, error)

	// MatchSession 检查给定的 sessionID 或前缀是否归属当前 Provider，若匹配返回对应的日志绝对路径
	MatchSession(sessionID string) (transcriptPath string, ok bool)

	// ReadRecentTurns 读取该会话最近的若干轮对话记录
	ReadRecentTurns(sessionID string, maxTurns int) ([]*TranscriptRecord, error)
}
