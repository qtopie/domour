# RFC-0004: Agent CLI Supervisor Extension & Antigravity Support

- **Status:** Under Review
- **Author:** Domour Agent Architecture Team
- **Created Date:** 2026-10-14
- **Target Spec:** `specs/modules/agent-supervisor.spec.md`

---

## 1. Summary

本提案设计并实现一个面向多实例 AI Agent CLI（首期深度支持 Antigravity CLI `agy`，同时支持抽象扩展至 Claude Code、Aider 等）的轻量级监控与调度扩展包 `ark/supervisor`。
通过 **“状态旁路采集 + Transcript 结构化日志监听 + 双模指令控制”** 的三层架构，彻底避开解析 TUI/PTY 屏幕 ANSI 乱码与交互弹窗卡死的高风险，实现对本地多个 Agent CLI 进程的：
1. **实时状态感知**：进程存活、阶段状态机（Thinking/ToolUse/AwaitingConfirmation/Idle）、Token 与配额消耗；
2. **无损结果采集**：直接监听结构化底层 `transcript`（JSONL），获取准确的思维链、工具调用与最终应答；
3. **双向指令调度**：支持对挂起工具调用的远程审批放行、流式 Headless 进程长连接双向交互，以及对已存在交互会话的指令续接（Resume）。

---

## 2. Motivation

在复杂工程场景中，开发者和自动化管道往往在单机上并发运行多个 Agent CLI 进程（例如多个 `agy` 实例分别处理不同的子系统任务）。直接对其进行监控调度面临以下核心痛点：
1. **终端 PTY 解析脆弱**：若通过抓取终端 stdout 屏幕输出，需处理 ANSI 转义序列、光标重绘以及各版本 UI 布局变化，极易失真。
2. **人工审批阻塞不可知**：Agent 在触发高危工具（如写入关键文件或执行 Shell 命令）时会进入确认等待，传统外部脚本难以感知是模型在耗时思考还是在等待回车，导致任务挂死。
3. **缺少统一调度中枢**：无法程序化集中查看所有 Agent 的进度，也无法程序化向特定 Agent 注入后续引导指令（Steering）或让空闲会话继续下一步。
4. **多 CLI 工具碎片化**：不同 CLI 工具（Antigravity, Claude Code, Aider 等）的启动参数、生命周期钩子和日志格式各异，亟需一套高内聚、易扩展的抽象 Supervisor 框架。

---

## 3. Detailed Design

### 3.1 分层架构

整个扩展包位于 `ark/supervisor/`，核心分为两层：**通用基础抽象层** 与 **CLI 特化适配器层**。

```
ark/supervisor/
├── types.go                // 通用领域模型与状态机定义
├── provider.go             // CLIProvider 适配器抽象接口
├── registry.go             // 进程发现与多实例状态注册表
├── telemetry.go            // 遥测采集服务（Unix Domain Socket / HTTP）
├── watcher.go              // Transcript 结构化日志监听器（基于 fsnotify）
├── controller.go           // 决策审批与指令下发器
├── supervisor.go           // 统一门面与生命周期管理
└── providers/              // 特化 Provider 目录
    ├── antigravity/        // Antigravity CLI (agy) 原生适配器
    │   ├── provider.go     // CLIProvider 接口实现
    │   ├── statusline.go   // agy statusline payload 解析
    │   ├── hook.go         // PreToolUse / PreInvocation 交互处理
    │   └── transcript.go   // transcript.jsonl 增量解析
    └── generic/            // 通用命令流式拉起 Provider
        └── provider.go
```

### 3.2 核心抽象接口契约 (SPI)

#### 1. 通用领域模型 (`types.go`)
- `ProcessStatus`: `StatusUnknown` | `StatusInitializing` | `StatusThinking` | `StatusWorking` | `StatusToolUse` | `StatusAwaitingConfirmation` | `StatusIdle` | `StatusTerminated`
- `AgentProcess`:
  - `ID`: 会话唯一标识 (`conversation_id` / `session_id`)
  - `PID`: 本地系统进程号
  - `ProviderName`: 归属适配器名称（如 `"antigravity"`）
  - `Workspace`: 工作区绝对路径 (CWD)
  - `Status`: 当前运行状态
  - `CurrentTool`: 正在执行或等待确认的工具名与摘要
  - `ToolPending`: 是否正在阻塞等待人工授权确认
  - `TranscriptPath`: 底层 JSONL 日志路径
  - `QuotaUsage`: Token / 额度消耗元数据
  - `LastHeartbeat`: 上次遥测心跳时间
- `ToolDecision`: `Allow` | `Deny` | `AskUser`
- `TranscriptRecord`: 单步结构化记录（Turn, Role, Content, ToolCalls, Usage）

#### 2. 适配器接口 (`AgentProvider`)
```go
type AgentProvider interface {
    // Name 返回适配器名称，例如 "antigravity"
    Name() string

    // ParseStatusline 将状态栏传入的原始 Payload 反序列化为通用 ProcessStatus 增量更新
    ParseStatusline(payload []byte) (*StatuslineUpdate, error)

    // ParseHookEvent 将生命周期 Hook 触发的事件反序列化为 HookRequest
    ParseHookEvent(event string, payload []byte) (*HookRequest, error)

    // FormatHookDecision 将统一审批结果格式化为适配该 Agent 的响应结构
    FormatHookDecision(req *HookRequest, decision ToolDecision, reason string) ([]byte, error)

    // ParseTranscriptLine 将 transcript.jsonl 的单行反序列化为 TranscriptRecord
    ParseTranscriptLine(line []byte) (*TranscriptRecord, error)

    // BuildHeadlessArgs 构造流式交互（Headless）启动命令行参数
    BuildHeadlessArgs(opts LaunchOptions) ([]string, error)

    // BuildResumeArgs 构造会话续接命令行参数
    BuildResumeArgs(sessionID string, prompt string, opts LaunchOptions) ([]string, error)
}
```

### 3.3 交互逻辑与三大数据流

1. **Telemetry Plane（遥测流）**：
   - Supervisor 在本地监听 Unix Domain Socket（默认 `/tmp/domour-supervisor.sock`）。
   - 各 CLI 进程在配置的状态栏脚本（`statusline`）或 Hook 中将 JSON 心跳发往该 Socket。
   - Supervisor 接收并更新 `Registry`，若心跳超时（默认 60s）未更新，标记进程可能失联；若状态显示 `tool_confirmation_pending: true`，立即触发审批警报事件。

2. **Transcript Plane（日志流）**：
   - 识别到 `TranscriptPath` 后，Supervisor 的 `TranscriptWatcher` 使用 `fsnotify` 追踪该文件增量字节。
   - 按行读取并调用对应的 `AgentProvider.ParseTranscriptLine`，提取思维链、ToolCall、输出文本，通过内部 EventBus 或 Channel 广播给外部订阅者。

3. **Control Plane（控制流）**：
   - **工具放行控制**：当 CLI 触发 `PreToolUse` Hook 时，向 Supervisor 提交审批挂起请求并等待响应。Supervisor 收到控制指令后，向 Hook 客户端返回 `{"decision": "allow"}` 放行继续执行，或 `{"decision": "deny"}` 拦截。
   - **Headless 流式控制**：由 Supervisor 通过 `exec.Command` 拉起的流式进程，保持长连接 `stdin/stdout`，直接写入单行 JSON 发送继续指令。
   - **会话接力续接**：对空闲的已存在交互会话，通过 `BuildResumeArgs` 拉起一次性续接任务带入上下文推进。

---

## 4. Antigravity CLI 特化集成规格

1. **Statusline 解析**：
   - 提取 `conversation_id`、`cwd`、`agent_state`、`tool_confirmation_pending`、`transcript_path`、`context_window`、`quota`。
   - 映射 `agent_state`:
     - `"thinking"` -> `StatusThinking`
     - `"tool_use"` -> 此时若 `tool_confirmation_pending == true` 则为 `StatusAwaitingConfirmation`，否则为 `StatusToolUse`
     - `"idle"` -> `StatusIdle`
2. **Hook 集成**：
   - 支持 `PreToolUse` 事件解析，提取 `tool_name`、`arguments`。
   - 支持输出 `{"decision": "auto_approve"}` 或 `{"decision": "deny", "reason": "..."}`。
   - 支持 `PreInvocation` 注入引导步（Steering）。
3. **Headless 启动模式**：
   - 参数矩阵：`agy -p "" --input-format stream-json --output-format stream-json --add-dir <workspace>`。

---

## 5. Alternatives Considered

1. **方案 A：使用 PTY 拦截并正则清洗 ANSI 码**
   - *拒绝原因*：不同终端大小会导致软折行，光标跳转和增量重绘极其复杂，且无法稳定识别工具确认弹窗。
2. **方案 B：仅支持 Antigravity 硬编码**
   - *拒绝原因*：限制了 Domour 作为通用智能体框架的扩展性，后续若引入 Claude Code 或自研 Agent CLI 将面临重构。

---

## 6. Security & Performance Considerations

- **本地 Socket 权限限制**：Unix Socket 权限严格设置为 `0600`，仅限当前用户读写，防止本地非授权访问。
- **并发与资源隔离**：Watcher 采用零拷贝滑动行读取，单文件只分配轻量协程；Registry 采用读写锁保护；心跳支持自动淘汰以防内存泄漏。
