# Module Spec: Agent CLI Supervisor Extension & Antigravity Support

## 1. Overview
本模块定义通用 Agent CLI 进程监控与调度扩展包 `ark/supervisor`。
该模块负责多 Agent CLI 进程的发现、实时状态与进度跟踪、结构化结果增量采集（Transcript Watcher）以及控制流调度（工具审批放行、流式 Headless 托管交互、会话续接）。
第一期全面深度适配 Antigravity CLI (`agy`)，并对未来第三方 CLI（如 Claude Code、Aider 等）提供可插拔接口。

---

## 2. Interface / API Contract

### 2.1 核心数据结构 (`types.go`)
- `ProcessStatus`: `StatusUnknown`, `StatusInitializing`, `StatusThinking`, `StatusWorking`, `StatusToolUse`, `StatusAwaitingConfirmation`, `StatusIdle`, `StatusTerminated`
- `AgentProcess`:
  ```go
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
  ```
- `ToolDecision`: `DecisionAllow`, `DecisionDeny`, `DecisionAskUser`
- `TranscriptRecord`:
  ```go
  type TranscriptRecord struct {
      StepIndex int                    `json:"step_index"`
      Type      string                 `json:"type"`
      Role      string                 `json:"role"`
      Content   string                 `json:"content"`
      ToolCalls []ToolCallInfo         `json:"tool_calls,omitempty"`
      Metadata  map[string]interface{} `json:"metadata,omitempty"`
  }
  ```

### 2.2 扩展点 SPI 接口 (`provider.go`)
```go
type AgentProvider interface {
    Name() string
    ParseStatusline(payload []byte) (*StatuslineUpdate, error)
    ParseHookEvent(event string, payload []byte) (*HookRequest, error)
    FormatHookDecision(req *HookRequest, decision ToolDecision, reason string) ([]byte, error)
    ParseTranscriptLine(line []byte) (*TranscriptRecord, error)
    FormatInstruction(prompt string) ([]byte, error)
    BuildHeadlessArgs(opts LaunchOptions) ([]string, error)
    BuildResumeArgs(sessionID string, prompt string, opts LaunchOptions) ([]string, error)
    MatchSession(sessionID string) (transcriptPath string, ok bool)
    ReadRecentTurns(sessionID string, maxTurns int) ([]*TranscriptRecord, error)
}
```

### 2.3 门面接口 (`supervisor.go`)
```go
type SpawnRequest struct {
    Provider   string `json:"provider,omitempty"`
    Prompt     string `json:"prompt"`
    Workspace  string `json:"workspace,omitempty"`
    Model      string `json:"model,omitempty"`
    Title      string `json:"title,omitempty"`
    PreferIDE  bool   `json:"prefer_ide,omitempty"`
    PreferCLI  bool   `json:"prefer_cli,omitempty"`
}

type SpawnResult struct {
    SessionID string   `json:"session_id"`
    PID       int      `json:"pid,omitempty"`
    Mode      string   `json:"mode"` // "agentapi_spawn", "cli_spawn", "ide_spawn"
    Provider  string   `json:"provider"`
    Args      []string `json:"args,omitempty"`
}

type InstructRequest struct {
    SessionID   string `json:"session_id"`
    Instruction string `json:"instruction"`
    Provider    string `json:"provider,omitempty"`
    Title       string `json:"title,omitempty"`
}

type InstructResult struct {
    SessionID   string `json:"session_id"`
    Status      string `json:"status"` // "sent", "resumed"
    Mode        string `json:"mode"`   // "stream_pipe", "agentapi_injected", "cli_spawn", "ide_spawn"
    Instruction string `json:"instruction"`
    PID         int    `json:"pid,omitempty"`
}

type Supervisor interface {
    RegisterProvider(p AgentProvider) error
    Start(ctx context.Context) error
    Stop() error
    ListProcesses() []*AgentProcess
    GetProcess(id string) (*AgentProcess, error)
    DiscoverSessions(ctx context.Context) ([]*AgentProcess, error)
    InspectSession(ctx context.Context, provider, sessionID string, maxTurns int) (*InspectResult, error)
    SpawnSession(ctx context.Context, req SpawnRequest) (*SpawnResult, error)
    InstructSession(ctx context.Context, req InstructRequest) (*InstructResult, error)
    WatchTranscript(ctx context.Context, processID string) (<-chan *TranscriptRecord, error)
    SubmitToolDecision(processID string, decision ToolDecision, reason string) error
    SendInstruction(ctx context.Context, processID string, prompt string) error
}
```

---

## 3. Acceptance Criteria (BDD)

### Feature 1: 通用架构与 Provider 适配

#### Scenario 1: [SPEC-SUPV-001] Provider 注册与进程发现
- **Given** 一个新初始化的 Supervisor 实例
- **When** 注册名为 `"antigravity"` 的 AgentProvider，并接收来自该 Provider 的首次进程心跳数据
- **Then** Registry 成功发现该实例，分配或记录对应的 SessionID 与 PID，并标记 Provider 为 `"antigravity"`
- **Mapped Test:** `testings/supervisor/supervisor_test.go:TestSupervisor_RegisterAndDiscover`

#### Scenario 2: [SPEC-SUPV-002] 状态机流转与挂起审批识别
- **Given** 一个处于运行中的 Agent 实例
- **When** 收到遥测更新 `agent_state: "tool_use"` 且 `tool_confirmation_pending: true`
- **Then** 实例状态正确更新为 `StatusAwaitingConfirmation`，`ToolPending` 标记为 `true`，且对外触发审批告警事件
- **Mapped Test:** `testings/supervisor/supervisor_test.go:TestSupervisor_ToolPendingDetection`

---

### Feature 2: Antigravity CLI 特化集成

#### Scenario 3: [SPEC-SUPV-003] Antigravity 状态栏与 Hook Payload 解析
- **Given** Antigravity CLI 发送的标准 statusline JSON 包含 `conversation_id`, `agent_state`, `transcript_path`, `quota`
- **When** AntigravityProvider 执行 `ParseStatusline`
- **Then** 正确提取工作区目录、状态枚举、Transcript 路径和 Token 配额百分比，无字段截断或丢失
- **Mapped Test:** `testings/supervisor/antigravity_test.go:TestAntigravityProvider_ParseStatusline`

#### Scenario 4: [SPEC-SUPV-004] Antigravity Transcript 增量日志解析
- **Given** `transcript.jsonl` 中追加了一条合法的 `PLANNER_RESPONSE` 记录（包含 `tool_calls` 与 `thinking`）
- **When** AntigravityProvider 执行 `ParseTranscriptLine`
- **Then** 成功反序列化为统一的 `TranscriptRecord`，准确还原 `StepIndex`、工具名及思考内容
- **Mapped Test:** `testings/supervisor/antigravity_test.go:TestAntigravityProvider_ParseTranscript`

---

### Feature 3: 控制调度与执行继续

#### Scenario 5: [SPEC-SUPV-005] 远程审批挂起与决策放行
- **Given** 某个 Agent 进程通过 PreToolUse Hook 发起拦截并挂起等待审批
- **When** 调用 `SubmitToolDecision(processID, DecisionAllow, "")`
- **Then** Hook 挂起通道被解除阻塞，输出合法的放行 JSON 格式（`decision: "allow"` / `"auto_approve"`），退出码为 0
- **Mapped Test:** `testings/supervisor/controller_test.go:TestController_ToolApproval`

#### Scenario 6: [SPEC-SUPV-006] 流式托管双向通信与指令下发
- **Given** 一个以 Headless stream-json 启动的 Agent 托管实例
- **When** 调用 `SendInstruction(ctx, processID, "继续执行测试")`
- **Then** 格式化 JSON 指令成功写入该子进程的标准输入管道 `stdin`，且不造成进程意外退出或挂起
- **Mapped Test:** `testings/supervisor/controller_test.go:TestController_SendInstructionStream`

#### Scenario 7: [SPEC-SUPV-007] CLI 命令行入口与交互控制
- **Given** 用户在终端执行 `domour supervisor run "say hello"` 或 `domour supervisor list`
- **When** 执行 `domour supervisor` 子命令
- **Then** CLI 正确解析参数，拉起或连接 Supervisor 引擎，成功驱动 Agent 实例执行并输出流式结果
- **Mapped Test:** `testings/supervisor/cli_test.go:TestSupervisor_CLI`

#### Scenario 8: [SPEC-SUPV-008] 本地正在进行的 Agent 实例自动发现与状态读取
- **Given** 系统中存在一个或多个正在运行的 Agent 进程（如持有着 presence lock 的 agy 实例）
- **When** 调用 `DiscoverRunningSessions()`
- **Then** 自动扫描发现活跃进程，提取 PID、工作目录 Workspace、会话 ID 以及最新执行进度，组装为 `AgentProcess` 列表
- **Mapped Test:** `testings/supervisor/antigravity_test.go:TestAntigravityProvider_DiscoverRunningSessions`

#### Scenario 9: [SPEC-SUPV-009] 多 Provider 会话检索与定向/自动探测 Inspect
- **Given** 用户想要检查某个特定 session_id 的详细对话历史
- **When** 执行 inspect 命令（支持传入 `-p <provider>` 显式指定，或缺省时由 Supervisor 依次探测匹配各 Provider）
- **Then** 能够准确命中拥有该会话的 Provider（如 Antigravity 或未来的 Copilot/Claude），并正确格式化展示最近多轮记录
- **Mapped Test:** `testings/supervisor/inspect_test.go:TestSupervisor_InspectMultiProviderRouting`

---

### Feature 4: Antigravity IDE 适配与多形态互通 (Dual-Variant Architecture: CLI & IDE)

#### Scenario 10: [SPEC-SUPV-010] Antigravity IDE 会话目录与客户端形态感知
- **Given** 系统中存在 Antigravity IDE 会话存储于 `.gemini/antigravity/brain` 或 `.gemini/antigravity-ide/brain`，或存在运行中的 Antigravity IDE 进程（`antigravity` / `Antigravity.exe`）
- **When** 调用 `MatchSession` 或 `DiscoverRunningSessions()`
- **Then** 成功识别该会话，并将 `ClientType` 标记为 `"ide"`（区别于 CLI 的 `"cli"`），正确提取其 Transcript 与状态
- **Mapped Test:** `testings/supervisor/antigravity_test.go:TestAntigravityProvider_IDEDualVariant`

#### Scenario 11: [SPEC-SUPV-011] IDE 专属指令下发与 AgentAPI / 桌面唤起
- **Given** 目标会话归属 Antigravity IDE 或当前机器无独立 `agy` CLI（如 Windows VM 或纯 IDE 环境）
- **When** 调用指令下发接口
- **Then** 优先通过 `language_server agentapi send-message` 或 IDE 执行入口进行调度，降级时不因缺失 `agy` CLI 而崩溃，返回明确的调用执行回执
- **Mapped Test:** `testings/supervisor/antigravity_test.go:TestAntigravityProvider_IDEInstructionDispatch`

---

### Feature 5: Agent Runtime 统一生命周期管理与 AgentAPI 强类型封装 (Unified Agent Runtime)

#### Scenario 12: [SPEC-SUPV-012] 统一会话多 Provider 自动发现 (DiscoverSessions)
- **Given** 本地运行着 1 个或多个不同 Provider 的 Agent 进程（CLI 或 IDE）
- **When** 调用 `Supervisor.DiscoverSessions(ctx)`
- **Then** Supervisor 自动汇总所有已注册 Provider 的活跃实例，返回包含 SessionID、PID、ClientType、Workspace 及实时状态的 `AgentProcess` 列表
- **Mapped Test:** `testings/supervisor/runtime_test.go:TestSupervisor_DiscoverSessions`

#### Scenario 13: [SPEC-SUPV-013] 统一任务拉起与多阶自适应启动 (SpawnSession)
- **Given** 调用方通过 `SpawnRequest` 传入新任务提示词、工作区与参数
- **When** 调用 `Supervisor.SpawnSession(ctx, req)`
- **Then** Runtime 自动完成多阶判定（优先 AgentAPI new-conversation，次选原生 CLI 异步后台拉起——网络由底层 vproxy 内核透明分流，Runtime 保持原生零污染，再降级为桌面 IDE 工作区唤起），返回分配的 `SpawnResult`，且子进程/会话被即时纳入 Registry
- **Mapped Test:** `testings/supervisor/runtime_test.go:TestSupervisor_SpawnSession`

#### Scenario 14: [SPEC-SUPV-014] 统一自适应指令下发与状态反馈 (InstructSession)
- **Given** 用户针对指定会话发送后续指令
- **When** 调用 `Supervisor.InstructSession(ctx, req)`
- **Then** Runtime 自动完成（Controller 流式管道 -> AgentAPI send-message -> CLI 续接 -> IDE 唤起）多阶自适应穿透，返回标准结构化 `InstructResult`
- **Mapped Test:** `testings/supervisor/runtime_test.go:TestSupervisor_InstructSession`

#### Scenario 15: [SPEC-SUPV-015] 强类型 AgentAPI 客户端封装 (Typed AgentAPI Client)
- **Given** 系统中存在活跃的 Antigravity 语言服务或 `agentapi` 可执行入口
- **When** 通过 `AgentAPIClient` 调用 `SendMessage`, `NewConversation`, `GetMetadata`
- **Then** 成功执行底层 RPC 交互并反序列化为结构化强类型对象（如 `AgentAPIMetadataResult`），错误时提供标准 Go error 包装
- **Mapped Test:** `testings/supervisor/agentapi_test.go:TestAgentAPIClient_TypedMethods`

---

### Feature 6: ChatGPT / Codex 统一 Provider 适配 (Unified ChatGPT & Codex Provider)

#### Scenario 16: [SPEC-SUPV-016] ChatGPT/Codex 本地会话库与运行实例发现 (DiscoverSessions)
- **Given** 本地存在 `~/.codex/state_5.sqlite` 数据库或正在运行的 `codex` / `app-server` 进程
- **When** 调用 `DiscoverRunningSessions()` 或 `Supervisor.DiscoverSessions(ctx)`
- **Then** 自动扫描发现活跃进程并读取 SQLite 中的 `threads` 会话记录，提取 PID、工作目录 Workspace、会话 ID 以及 Token 消耗，组装为 `AgentProcess` 列表，Provider 标识为 `"chatgpt"`
- **Mapped Test:** `testings/supervisor/chatgpt_test.go:TestChatGPTProvider_DiscoverSessions`

#### Scenario 17: [SPEC-SUPV-017] ChatGPT/Codex 结构化会话回溯与定向 Inspect (MatchSession & ReadRecentTurns)
- **Given** 用户想要回溯某个特定 ChatGPT/Codex 会话历史
- **When** 调用 `MatchSession(sessionID)` 并调用 `ReadRecentTurns(sessionID, maxTurns)`（或通过 `domour supervisor inspect -p chatgpt <id>`）
- **Then** 准确匹配该会话并读取对应的持久化记录（rollout/sqlite），将多轮消息、工具执行与思维过程反序列化为标准的 `[]*TranscriptRecord`
- **Mapped Test:** `testings/supervisor/chatgpt_test.go:TestChatGPTProvider_MatchAndInspect`

#### Scenario 18: [SPEC-SUPV-018] ChatGPT/Codex 事件流转录与 JSONL 增量解析 (TranscriptParser)
- **Given** `codex exec --json` 产生的单行 JSONL 输出（如 agent_message, tool_call, tool_result 等）
- **When** 调用 `ParseTranscriptLine(line)`
- **Then** 正确反序列化并映射为通用的 `TranscriptRecord`，保留 Role、Content 与 ToolCalls 信息
- **Mapped Test:** `testings/supervisor/chatgpt_test.go:TestChatGPTProvider_ParseTranscriptLine`

#### Scenario 19: [SPEC-SUPV-019] ChatGPT/Codex App-Server JSON-RPC 通信与双向控制 (AppServer Client)
- **Given** 本地运行着 `codex app-server` (支持 stdio 或 unix socket)
- **When** 客户端调用 `ThreadStart`, `TurnStart`, 或处理 `Approval` 决策
- **Then** 按照标准 JSON-RPC 2.0 格式进行请求封装与响应解析，支持非阻塞流式通知分发与命令审批放行
- **Mapped Test:** `testings/supervisor/chatgpt_test.go:TestChatGPTProvider_AppServerRPC`

