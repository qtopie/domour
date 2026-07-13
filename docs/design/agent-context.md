# Domour Agent 上下文模型设计

> Agent 上下文是指 Agent 在执行过程中能 "看到" 的全部信息，以及这些信息如何在多个子 Agent 之间流动。
> 本设计遵循"不复用 Dapr 已提供的能力"原则。

---

## 一、背景与问题

> **上下文工程**是 Agent 开发中的核心基础设施。以下章节总结其作用与当前架构的实现状态。

### 上下文工程的作用

在 Agent 开发中，上下文工程是决定 Agent 行为边界和协作效率的基础设施，其核心作用包括：

| 维度 | 说明 |
|------|------|
| **视野控制** | 定义 Agent 能"看到"什么、不能看到什么，避免信息过少（遗漏关键决策依据）或过多（Token 爆炸、注意力分散） |
| **通信协议** | 多 Agent 协作时，定义父子 Agent 间信息的传递、隔离与回写规则，确保子 Agent 私有推理不泄露，共享状态能同步 |
| **Token 经济** | LLM 上下文窗口是稀缺资源，通过压缩（摘要、截断、Masking）、优先级分层（保护最近轮次和活跃任务）、分级存储（STM + LTM）来实现高效利用 |
| **持久化方案** | 进程重启后 Agent 能否恢复状态、跨进程能否共享同一上下文，取决于上下文的序列化、存储与加载策略 |

**一句话总结**：上下文工程 = 视野控制 + 通信协议 + Token 经济 + 持久化方案，是 Agent 正确性、效率和可扩展性的基石。

### 实现状态概览

| 系统 | 实现状态 | 说明 |
|------|---------|------|
| **MemoryContextManager (Tier 1~3)** | ✅ 已实现 | 全局记忆(~/.domour/*.md)、项目记忆(.domour/*.md)、JIT 文件发现，带 L1 缓存 |
| **ContextManager (Per-Agent)** | ✅ 已实现 | Pristine Graph、STM 环状缓冲区、Pipeline 压缩链、4种渲染方法 |
| **ContextBridge (跨 Agent 通信)** | ✅ 已实现 | 基于 Channel 的 Pub/Sub 模型，Scope 隔离，定向投递 |
| **Pipeline 处理器链** | ✅ 已实现 | ToolMasking → BlobDegradation → NodeDistillation → NodeTruncation |
| **L1 共享缓存 (ContextCache)** | ✅ 已实现 | 双 TTL 层级（30min 长缓存 / 5min 短缓存），覆盖 Tier 1~3、Session、Topic、Agent |
| **模式自适应 (Mode-Aware)** | ✅ 已实现 | deep_think/performance/survival/balanced 四模式动态调整 STM 参数和 Pipeline 开关 |
| **Session 持久化 (SurrealDB)** | ✅ 已实现 | 三层缓存（L1→L2→DB），跨进程重启数据不丢失，SurrealDB 3.x 兼容 |
| **静态提示词前缀缓存** | ✅ 已实现 | System message = 纯静态 (Tier1 + Tier2)，JIT/History/Input 放在 User message 中，保证 KV cache 命中 |
| **跨 Agent 上下文剥离** | ✅ 已实现 | Cerebellum → 仅意图+工具 schema，Diencephalon → 仅渲染消息，Brainstem → 零上下文 |
| **LTM (Memorize/Recall)** | ⏳ 待实现 | 接口已定义，暂无向量存储/语义检索后端 |
| **Dapr State Store 集成** | 🔄 可选 | 现有 SurrealDB 直接连接模式已可用；Dapr sidecar 方案作为备选 |

### 总体目标

> **为 Domour 建立一个多 Agent 协同、模式自适应、模块感知、多 LLM 兼容、支持本地离线/集群/隐私部署、且安全可拦截的 Agent 上下文工程。**

| 维度 | 说明 |
|------|------|
| **多 Agent 协同** | Brain/Cerebellum/Brainstem/Diencephalon 四层脑区的上下文隔离与互通，父子 Agent 按策略注入/同步 |
| **高效支持不同模式任务** | 9 种运行模式下动态调整策略，按模式需求决定压缩比、保留轮次、持久化行为 |
| **自适应不同 Agent 模块要求** | 不同脑区接收不同粒度的上下文——Brain 获取全貌+长期记忆，Cerebellum 获取任务+工具历史，Brainstem 仅获取安全规则+最小必要信息 |
| **支持多种底层 LLM 能力** | 适配 DeepSeek/Gemini/OpenAI/本地模型的不同上下文窗口与定价，按模型计算 Token 预算 |
| **支持本地离线、集群模式及隐私模式** | 离线缓存+增量同步、Dapr Pub/Sub 跨进程广播、Stealth 模式加密传输三种部署全面覆盖 |
| **支持安全可拦截** | Brainstem veto + 可审计标签，安全过滤器嵌入管道处理器链 |

### 目标详解

#### 1. 模式感知（Mode-Aware）—— 按运行模式切换策略

Domour 有 9 种运行模式，对上下文的需求截然不同：

| 模式 | 上下文策略 |
|------|-----------|
| **Deep Think** | 保留完整推理链，无压缩 |
| **Performance** | 极致对话性能，充分利用资源既保证会话质量又保证效率 + 活跃工具 |
| **Survival** | 纯本地 LLM 小窗口，强摘要 + 高压缩比 |
| **Hibernate** | 上下文全量持久化后卸载 |
| **Stealth** | 上下文内容加密存储与传输 |

→ 上下文系统不能在编译时固定一套策略，而是根据当前模式**动态调整管道参数**。

#### 2. 四层架构感知 —— 不同脑区不同上下文形态

| 组件 | 上下文关注点 | 需注入的内容 |
|------|-------------|-------------|
| **Brain (Cerebrum)** | 宏观规划、反思 | 全部三层 + 长期记忆 |
| **Cerebellum** | ReAct 循环、工具编排 | 当前任务描述 + 工具调用历史 |
| **Brainstem** | 安全拦截、系统调用 | 安全规则 + 最小必要信息 |
| **Diencephalon** | 模型适配、格式转换 | 与目标 Provider 窗口匹配的格式化输出 |

→ 父子 Agent 间不是简单注入，而是**按接收方角色裁剪上下文**。

#### 3. Multi-Provider 适配 —— 上下文与 LLM 能力对齐

- DeepSeek、Gemini、OpenAI 的上下文窗口和定价不同
- 上下文系统应在 Diencephalon 层做**模型感知的 Token 预算计算**，而非一刀切
- 例如：给 DeepSeek-R1 传 1M Token 的上下文是可以的，但给本地小模型传超过 8K 会直接溢出

#### 4. 边缘同步 —— 离线上下文处理

- Edge 端进程可能处于 Survival 模式（无云端 LLM）
- 上下文系统需要支持**离线缓存 + 上线后增量同步**
- 事件溯源（Event Sourcing）模式：本地记录操作日志，恢复时重放重建上下文

#### 5. 安全上下文 —— 可审计、可拦截

- Brainstem 的 veto 机制需要上下文带有**安全标签**
- 每个 ContextNode 应携带安全等级标注
- 管道处理器中应有**安全过滤器**（在 Masking 之前检查 veto 条件）

### 具体目标

在上述总体目标下，本次设计聚焦以下 5 个可量化的具体目标：

1. **三层上下文**：全局（Global）→ 局部（Local）→ 隔离（Isolated）
2. **按需透传**：子 Agent 创建时按策略注入上下文，非隐式全量复制
3. **跨进程共享**：Dapr State Store 提供持久化，Dapr Pub/Sub 提供同步
4. **Token 预算管理**：Pipeline 处理器链自动压缩
5. **短/长期记忆**：STM（窗口保留）+ LTM（持久化 + 摘要）

---

## 二、两层架构 + 三层作用域

### 2.1 两层存储架构

```
┌──────────────────────────────────────────────────────────────────────┐
│  ① MemoryContextManager（全局共享层 — 跨所有 Agent 共享同一个实例）    │
│                                                                       │
│  Tier 1: System Instruction (System Prompt)                          │
│    ├── 全局规则 ~/.domour/*.md           [L1 缓存: 30min TTL]        │
│    └── Skills/工具元数据                  [按需动态注入]               │
│                                                                       │
│  Tier 2: Project Memory (Project Context)                            │
│    └── 项目缓存 <.project>/.domour/*.md  [L1 缓存: 30min TTL]        │
│                                                                       │
│  Tier 3: JIT File Discovery (按需发现)                               │
│    └── discoverContext(accessedPath)     [L1 缓存: 5min TTL]         │
└──────────────────────────────────────────┬───────────────────────────┘
                                           │ 共享 (Config, L1 Cache)
┌──────────────────────────────────────────▼───────────────────────────┐
│  ② ContextManager（每 Agent 实例独有 — 隔离的会话上下文）              │
│                                                                       │
│  Pristine Graph（不可变备份）                                         │
│  ┌──────────────────────────────────────────────────────────┐        │
│  │  Nodes: map[string]*ConcreteNode                        │        │
│  │  Edges: map[string][]string                             │        │
│  │  用途: "真实发生了什么" 的原始记录，用于对比/回退         │        │
│  └──────────────────────────────────────────────────────────┘        │
│                                                                       │
│  STM 环状缓冲区（工作区 — 可操作/可压缩）                             │
│  ┌──────────────────────────────────────────────────────────┐        │
│  │  保护区域: 最近 N 轮（跳过 Pipeline 处理）                │        │
│  │  可压缩区域: 更旧轮次（按需蒸馏/截断/摘要）                │        │
│  └──────────────────────────────────────────────────────────┘        │
│                                                                       │
│  Pipeline 处理器链（压缩/掩码/蒸馏 — mode 感知）                     │
│  ┌──────────────────────────────────────────────────────────┐        │
│  │  ToolMasking → BlobDegradation → NodeDistillation →     │        │
│  │  NodeTruncation                                          │        │
│  └──────────────────────────────────────────────────────────┘        │
│                                                                       │
│  ContextBridge（跨 Agent 通信 — 用 Channel Pub/Sub 隔离）            │
└──────────────────────────────────────────────────────────────────────┘
```

### 2.2 三层作用域与访问控制

| 作用域 | 对应组件 | 可见性 | 持久化 |
|--------|---------|--------|--------|
| **Global** (Tier 1~3) | MemoryContextManager | 所有 Agent 共享（只读） | 文件系统 ~/.domour/ + L1 缓存 |
| **Local** (Session/Topic) | ContextManager + ContextBridge | 同 Session 内所有 Agent（读写，通过 Channel） | SurrealDB Session Manager |
| **Isolated** (单 Agent) | ContextManager Pristine+STM | 仅所属 Agent（完全隔离） | 进程内（重启丢失） |

### 2.3 作用域隔离规则

| 操作 | Global | Local | Isolated |
|------|--------|-------|----------|
| Agent A 可读 | ✓ | ✓（通过 Bridge Channel） | ✗ |
| Agent A 可写 | ✗ | ✓（通过 Bridge.Publish） | ✓（仅自己） |
| Agent B 可读 | ✓ | ✓（需订阅 Channel） | ✗ |
| 跨 Agent 同步 | 天然共享 | Bridge.Publish/Subscribe | PublishDirect（定向投递） |

---

## 三、核心数据结构

### 3.1 Node — 实际代码 `ConcreteNode`

```go
type NodeType string

const (
    NodeUnknown        NodeType = "unknown"
    NodeInstruction    NodeType = "instruction"
    NodeConversation   NodeType = "conversation"
    NodeObservation    NodeType = "observation"
    NodeAction         NodeType = "action"
    NodePlan           NodeType = "plan"
    NodeReflection     NodeType = "reflection"
    NodeSummary        NodeType = "summary"
    NodeSnapshot       NodeType = "snapshot"       // 摘要合成节点
    NodeToolResult     NodeType = "tool_result"
)

type ContextScope string

const (
    ScopeGlobal   ContextScope = "global"
    ScopeLocal    ContextScope = "local"
    ScopeIsolated ContextScope = "isolated"
)

// ConcreteNode 是 ContextGraph 中实际使用的节点
type ConcreteNode struct {
    ID        string            `msgpack:"id"`
    Type      NodeType          `msgpack:"type"`
    AgentID   string            `msgpack:"agent_id"`
    SessionID string            `msgpack:"session_id"`
    Scope     ContextScope      `msgpack:"scope"`
    Turn      int               `msgpack:"turn"`
    Priority  int               `msgpack:"priority"`    // 0~100
    TokenSize int               `msgpack:"token_size"`
    Content   string            `msgpack:"content"`
    Metadata  map[string]string `msgpack:"metadata"`
    Tags      []string          `msgpack:"tags"`
    CreatedAt int64             `msgpack:"created_at"`
    children  []*ConcreteNode   // Graph 内部使用，不序列化
}
```

### 3.2 Graph — 实际代码 `ContextGraph`

```go
type ContextGraph struct {
    mu    sync.RWMutex
    Nodes map[string]*ConcreteNode
    Edges map[string][]string   // NodeID → 子 NodeID 列表
    Roots []string              // 无入边的节点（入口点）
}
```

图操作直接在 `ContextGraph` 上，无需 `GraphBuilder`：

```go
func (g *ContextGraph) AddNode(node *ConcreteNode) error
func (g *ContextGraph) RemoveNode(nodeID string) 
func (g *ContextGraph) AddEdge(parentID, childID string) error
```

### 3.3 STM 配置 — 实际代码 `STMConfig`

```go
type STMConfig struct {
    Capacity         int // 环状缓冲区总容量（默认 1024）
    ProtectedTurns   int // 保护区域轮次，跳过 Pipeline（默认 2）
    TokenBudget      int // 渲染后最大 Token 预算（默认 4096）
    SummaryInterval  int // 每隔 N 轮触发一次主动摘要
    CompressionLevel int // 压缩力度 1~10（默认 5）
}
```

### 3.4 共享缓存 — 实际代码 `ContextCache`

```go
// 双 TTL L1 缓存（Otter 库实现）
type ContextCache struct {
    longCache  *otter.Cache[string, any]  // 30min — Tier 1/2 固定内容
    shortCache *otter.Cache[string, any]  // 5min  — Tier 3 JIT / Session / Topic
}

// 缓存键层级:
//   global:project:config            → 项目级静态配置
//   memory:tier1:global              → Tier 1 系统指令
//   memory:tier2:project:{name}      → Tier 2 项目记忆
//   memory:tier3:{session}:{path}    → Tier 3 JIT 文件发现
//   session:{id}:conversation        → 会话对话历史
//   session:{id}:agent:{aid}         → Agent 私有上下文
//   session:{id}:topic:{tid}         → Topic 级共享上下文
//   agent:{id}:state                 → Agent 运行状态
```

---

## 四、ContextManager（已实现）

### 4.1 职责

- 维护每个 Agent 实例独立的 Pristine Graph（不可变原始记录）
- 管理 STM 环状缓冲区（保护区域 + 可压缩区域）
- 运行 Pipeline 处理器链（mode 感知）
- 通过 ContextBridge 实现跨 Agent 上下文共享
- 使用 4 种渲染方法适配不同 Agent 层的阅读权限

### 4.2 结构 (代码)

```go
type ContextManager struct {
    sessionID    string             // 所属会话
    topicID      string             // 所属话题
    agentID      string             // 所属 Agent
    bridge       *ContextBridge     // 跨 Agent 通信桥

    pristine     *ContextGraph      // 不可变原始图备份
    stmBuffer    []*ConcreteNode    // 环状工作缓冲区
    stmHead      int                // 当前写入位置
    stmProtected int                // 保护边界索引
    pipeline     *PipelineOrchestrator // 压缩/掩码/蒸馏链
    config       STMConfig          // Token 预算/模式等配置
    mu           sync.RWMutex
}
```

### 4.3 核心流程

```
初始化
  └→ NewContextManager(sessionID, topicID, agentID)
       ├── ContextBridge (共享引用)
       ├── Pristine Graph (空)
       ├── STM 环状缓冲区 (1024 cap)
       └── PipelineOrchestrator (模式感知)

每次新消息
  └→ Assemble(ctx, tier1, tier2, tier3, userInput)
       ├── 加载系统提示 (Tier 1 + Tier 2 → system message)
       ├── 加载 JIT 上下文 (Tier 3)
       ├── 写入 STM 缓冲区 (用户输入 + 前次回复)
       ├── 执行 Pipeline 处理器链 (仅处理可压缩区域)
       ├── 写入 Pristine Graph (完整副本)
       └── 返回 AssembledContext

渲染
  └→ RenderFor*(ctx, assembled)
       ├── RenderForAPI       → 结构化 []Message (+ 模型信息)
       ├── RenderForCLI       → 纯文本
       ├── RenderForCerebellum → 意图 + 工具 schema + 当前步骤
       └── RenderForDiencephalon → 精简 []Message + provider 信息
```

### 4.4 模式感知配置

```go
func (m *ContextManager) applyModeConfig(mode Mode) {
    switch mode {
    case ModeDeepThink:
        m.config.ProtectedTurns = 5    // 保护更多轮次
        m.config.TokenBudget = 32000   // 大 Token 预算
    case ModePerformance:
        m.config.ProtectedTurns = 1    // 仅保护最近 1 轮
        m.config.TokenBudget = 4000    // 严格控制预算
        m.pipeline.SetProcessors([]string{"masking", "truncation"}) // 只做掩码+截断
    case ModeSurvival:
        m.config.TokenBudget = 2048    // 极小预算
        m.config.CompressionLevel = 10 // 最大压缩
    case ModeBalanced:
        m.config.ProtectedTurns = 2    // 默认保护 2 轮
        m.config.TokenBudget = 8192    // 适中预算
    }
}
```

### 4.5 提示词前缀缓存策略（关键）

```
[system]  ← Tier 1 (global rules + skills) + Tier 2 (project .domour)
            └── 纯静态内容，跨所有 Agent 和调用一致
                → LLM KV Cache 命中，大幅降低首 Token 延迟

[user]    ← Tier 3 (JIT file discovery)
            └── 按需动态加载（accessedPath 触发）
                → L1 缓存 5min TTL

[user]    ← STM buffer (对话历史)
            └── 随每次调用变化

[user]    ← 当前用户输入
```

---

## 五、Pipeline 处理器链（已实现）

### 5.1 处理器接口

```go
type PipelineProcessor interface {
    Name() string
    Process(ctx context.Context, buffer *PipelineBuffer) error
}
```

### 5.2 默认处理器链

| 序号 | 处理器 | 触发条件 | 功能 | 代码位置 |
|------|--------|----------|------|----------|
| 1 | **ToolMasking** | 每次新消息 | 超长工具输出（>8000 tokens）→ 摘要代替原内容 | `pipeline.go:155` |
| 2 | **BlobDegradation** | 每次新消息 | 大二进制数据（图片 base64）→ 占位描述 | `pipeline.go:206` |
| 3 | **NodeDistillation** | Token 超限 | >15000 tokens 的大节点 → 提取关键信息 | `pipeline.go:257` |
| 4 | **NodeTruncation** | Token 超限 | >4000 tokens 的节点 → 保留首尾 | `pipeline.go:308` |

### 5.3 处理区域

```
STM 环状缓冲区
┌───────────────────────────────────────┐
│  保护区域 (protected turns)            │  ← Pipeline 跳过
│  [Turn-1] [Turn-2]                    │
├───────────────────────────────────────┤
│  可压缩区域                            │  ← Pipeline 处理
│  [Turn-3] [Turn-4] ... [Turn-N]       │
└───────────────────────────────────────┘
```

处理器仅对 **可压缩区域** 执行，保护区域（最近 N 轮对话 + 活跃任务）始终保持完整。

### 5.4 保护策略

| 保护类型 | 识别方式 | 原因 |
|----------|----------|------|
| `recent_turn` | Turn <= protectedTurns | 保留最近交互上下文 |
| `active_task` | Node.Priority >= 80 | 避免截断进行中的任务 |
| `system_instruction` | NodeType == instruction | 核心指令不被压缩 |

---

## 六、MemoryContextManager（已实现）

> 注意：这是**全局共享的记忆层**（MemoryContextManager），非每 Agent 实例化。
> ContextManager 在 `Assemble()` 时引用它的 `LoadSystemPrompt()` 和 `DiscoverContext()` 输出。

### 6.1 结构

```go
type MemoryContextManager struct {
    cache         *ContextCache     // 双 TTL L1 缓存
    store         *session.Manager  // session 持久化管理器（可选）
    tier1         MemoryTier        // 全局规则 ~/.domour/*.md
    tier2         MemoryTier        // 项目记忆 .domour/*.md
    tier3         MemoryTier        // JIT 文件发现
    ltm           *LTMMemory        // 长期记忆（暂为 stub）
}

type MemoryTier struct {
    id       string
    basePath string
    files    map[string]*FileMeta
}

type LTMMemory struct {
    // Recall/Memorize — 接口已定义，尚未实现向量存储后端
}
```

### 6.2 三层加载策略

| 层级 | 内容 | 加载时机 | 缓存 |
|------|------|----------|------|
| **Tier 1** | 全局规则：`~/.domour/*.md` | Agent 启动时加载 | L1 30min 长缓存 |
| **Tier 1** | Skills 元数据 (Tools schema) | 按需动态注入 | — |
| **Tier 2** | 项目记忆：`.domour/*.md` | Agent 启动时加载 | L1 30min 长缓存 |
| **Tier 3** | JIT 文件发现 | `discoverContext(accessedPath)` 触发 | L1 5min 短缓存 |

### 6.3 LoadSystemPrompt — 静态前缀生成

```go
func (m *MemoryContextManager) LoadSystemPrompt(ctx context.Context, sessionID string) string {
    // 输出 = identity + skills_section + interception_note
    // 纯文本，不包含任何动态内容（会话历史、用户输入等）
}
```

这是 LLM 请求中 `messages[0]`（system role）的内容。设计目标：
- **完全静态**：同一个 session 内的所有调用产生 byte-identical 输出
- **跨 Agent 共享**：Cerebrum/Cerebellum/Diencephalon 使用同一份
- **最大化 KV cache 命中**：不随对话长度变化

### 6.5 计划中的 LTM 存储策略

未来 LTM 将依赖向量存储做语义检索（可选 Dapr Vector Store 或直接向量数据库）：

| 策略 | 描述 | 适用场景 |
|------|------|----------|
| **RollingSummary** | LLM 生成滚动摘要，每次新摘要基于前一次 + 新内容 | 长对话持久化 |
| **SimpleTruncation** | 保留首尾中间截断，标注 `[truncated N tokens]` | 低资源场景 |
| **SemanticChunk** | 按语义边界切分 + 向量化 → 向量存储 | 语义检索场景 |

---

## 七、ContextBridge — 跨 Agent 上下文透传（已实现）

> 跨 Agent 通信不是通过注入 ContextGraph（注入策略旧方案已废弃），而是通过 **ContextBridge** 的 Channel Pub/Sub 模型 + **RenderFor\*** 方法实现的阅读权限隔离。

### 7.1 ContextBridge 架构

```
Cerebrum Agent (Brain)
  │
  ├── ContextManager
  │    ├── Pristine Graph (完整信息: 推理 + 计划 + 观察)
  │    ├── STM Buffer
  │    └── RenderForCerebellum → 意图 + 工具 + 当前步骤 (剥离推理细节)
  │
  ├── ContextBridge ── Publishes → Channel: "session:{id}:cerebellum"
  │       │
  │       ▼ 订阅
  │
Cerebellum Agent
  │
  ├── ContextManager
  │    ├── Pristine Graph
  │    ├── STM Buffer
  │    └── RenderForBrainstem → 仅指令 (剥离详情)
  │
  ├── ContextBridge ── Publishes → Channel: "session:{id}:diencephalon"
  │       │
  │       ▼ 订阅
  │
Diencephalon Agent
  │
  ├── ContextBridge ── Subscribes → 必要上下文 (不含当前详情)
  │
  └── Brainstem (Motor) — 不共享上下文，仅接收指令
```

### 7.2 ContextBridge 接口

```go
type ContextBridge struct {
    mu       sync.RWMutex
    channels map[string]map[string]chan *BridgeMessage  // topicID → {agentID → chan}
    acl      map[string]*ChannelACL                      // topicID → ACL
}

func NewContextBridge() *ContextBridge

// Publish 将消息发布到指定 topic（所有订阅者收到）
func (b *ContextBridge) Publish(topicID string, msg *BridgeMessage)

// Subscribe 订阅一个 topic（返回接收 channel）
func (b *ContextBridge) Subscribe(agentID, topicID string) <-chan *BridgeMessage

// PublishDirect 定向发送给特定 Agent
func (b *ContextBridge) PublishDirect(targetAgentID string, msg *BridgeMessage)

// AddScopeACL 设置 topic 的 Scope 访问控制
func (b *ContextBridge) AddScopeACL(topicID string, acl *ChannelACL) error
```

### 7.3 各 Agent 的上下文可见性矩阵

| Agent | 看到的内容 | 实现方式 | 设计理由 |
|-------|-----------|----------|----------|
| **Brain (Cerebrum)** | 完整：会话历史、推理链、计划、观察、反思 | `ContextManager.RenderForAPI()` 全量渲染 | 大脑需要完整上下文做规划和反思 |
| **Cerebellum** | 意图 + 工具 schema + 当前执行步骤 | `ContextManager.RenderForCerebellum()` 剥离推理细节 | 小脑执行战术，不需知道为何做，只需知道做什么 |
| **Diencephalon** | 精简消息 + provider 信息 | `RenderForDiencephalon()` 仅返回必要消息 | 间脑是中继，不参与推理 (原文: "间脑知道必要上下文就好了") |
| **Brainstem (Motor)** | 零上下文 — 仅接收指令 | 不调用任何 Render 方法 | 脑干只执行安全拦截和系统调用 (原文: "脑干不共享当前详情") |

### 7.4 渲染方法对比

```go
// RenderForAPI — 完整上下文给 LLM Provider (Cerebrum)
// messages[0]: system (Tier 1 + Tier 2) → 纯静态 → KV Cache 命中
// messages[1..N]: conversation history → 完整会话
// messages[N+1]: user input
func (m *ContextManager) RenderForAPI(ctx context.Context, ac *AssembledContext) ([]Message, ProviderInfo)

// RenderForCerebellum — 仅执行所需的最小上下文
// 输出: intent + tool schemas + current step
func (m *ContextManager) RenderForCerebellum(ctx context.Context, ac *AssembledContext) *CerebellumContext

// RenderForDiencephalon — 间脑中继用
// 输出: 精简 []Message + provider info (无推理过程)
func (m *ContextManager) RenderForDiencephalon(ctx context.Context, ac *AssembledContext) *DiencephalonContext

// RenderForCLI — CLI 交互用纯文本
func (m *ContextManager) RenderForCLI(ctx context.Context, ac *AssembledContext) string
```

### 7.5 隔离保证

| 保证 | 机制 |
|------|------|
| **Cerebellum 看不到推理链** | `RenderForCerebellum` 仅提取 intent + tool schema |
| **Brainstem 看不到上下文** | Brainstem 不持有 ContextManager 引用 |
| **跨 Agent 不同步 Pristine** | 每个 Agent 有独立 Pristine Graph + STM |
| **Bridge 需要显式订阅** | 无隐式广播，Agent 必须 `Subscribe` 才能接收 |
| **ACL 控制 Channel 访问** | `AddScopeACL` 设置每个 topic 的读写权限 |

---

## 八、Session 持久化 — SurrealDB（已实现）

### 8.1 三层缓存架构

```
MemoryContextManager (Tier 1~3)      ← 文件系统 + L1 缓存
         │
session.Manager                     ← 会话状态管理
  ├── L1: 进程内 Otter 缓存 (Instant)
  ├── L2: SurrealDB 缓存表 (24h TTL)  [可选]
  └── DB: SurrealDB 会话表 (持久化)    [仅 Core 模式]
```

### 8.2 持久化流程

```go
// SaveSession
// 1. Upsert to SurrealDB (with RecordID for hyphens)
// 2. Update L2 cache
// 3. Set L1 cache
// 4. Emit event via EventBus

// GetSession
// 1. Try L1 cache
// 2. Try L2 cache
// 3. Query SurrealDB (SELECT * FROM $id  with RecordID param)
// 4. L2 miss → return fresh empty session (never "not found" error)
```

### 8.3 关键修复

| 问题 | 修复 | 文件 |
|------|------|------|
| SurrealDB 3.x `type::thing` 移除 | 改用 `type::record` (注意：`FROM` 后不能用) | `surreal.go` |
| 带连字符 ID 被解析为减法 | 必须使用 `models.NewRecordID("table", id)` + `SELECT * FROM $id` | `surreal_store.go` |
| `Query[any]` 空结果集返回 `[{Session{ID:""}}]` | 在 unmarshal 路径增加 `len(id) > 0` 检查 | `session/manager.go` |
| 空 ID 被保存到 L2 缓存 | `SaveSession` 入口处增加 `len(session.ID) == 0` 校验 | `session/manager.go` |

### 8.4 模式切换

```go
// Core 模式: 使用 SurrealDB (持久化)
// Standalone 模式: 使用 MemoryStore (进程内) 或 BadgerStore (磁盘)

switch mode {
case CoreMode:
    store = NewSurrealStore(surrealDB)
case DevMode:
    store = NewMemoryStore()
case StandaloneMode:
    store = NewBadgerStore(path) // 文件级持久化
}
```

---

## 九、集成到现有 Domour 架构

### 9.1 组件定位

| Domour 组件 | 上下文组件 | 关系 |
|-------------|-----------|------|
| Diencephalon | MemoryContextManager | 读取 Tier 1~3 静态提示词 |
| Cerebrum (Brain) | ContextManager (完整) | 全量 Pristine + STM + RenderForAPI |
| Cerebellum | ContextManager (剥离) | RenderForCerebellum → 仅意图+工具 |
| Brainstem | 无 ContextManager | 仅接收安全拦截指令 |
| Engine | session.Manager | 初始化和持久化 |
| Diencephalon 中继 | ContextBridge | Broker 角色连接各 Agent |

### 9.2 数据流

```
用户输入
  │
  ▼
Diencephalon
  │  ├── MemoryContextManager.LoadSystemPrompt()  → system message (Tier 1 + 2)
  │  ├── MemoryContextManager.DiscoverContext()    → JIT context (Tier 3)
  │  └── ContextBridge.Subscribe()                 → 来自 Cerebrum 的同步
  │
  ▼
Cerebrum (Brain)
  │  ├── ContextManager.Assemble()    → 组装完整上下文
  │  ├── ContextManager.RenderForAPI() → → → LLM Provider
  │  └── ContextBridge.Publish()      → 处理后透传给 Cerebellum
  │
  ▼
Cerebellum
  │  ├── ContextManager.RenderForCerebellum() → 仅意图+工具 schema
  │  └── ContextBridge.Publish()       → 执行结果回传
  │
  ▼
Brainstem (Motor)
     └── 直接执行，不关心上下文
```

---

## 十、API 设计（已完成的内核 API）

```go
// ---- MemoryContextManager API ----

// LoadSystemPrompt 加载系统提示词（Tier 1 + Tier 2，纯静态）
func (m *MemoryContextManager) LoadSystemPrompt(ctx context.Context, sessionID string) string

// DiscoverContext JIT 文件发现（Tier 3）
func (m *MemoryContextManager) DiscoverContext(ctx context.Context, accessedPath string) ([]byte, error)

// ---- ContextManager API ----

// Assemble 组装一次完整的上下文（Tier 1~3 + STM + 用户输入）
func (m *ContextManager) Assemble(ctx context.Context, tier1, tier2 string, tier3 []byte, userInput string) (*AssembledContext, error)

// RenderForAPI 渲染为 LLM Provider 可用的消息格式
func (m *ContextManager) RenderForAPI(ctx context.Context, ac *AssembledContext) ([]Message, ProviderInfo)

// RenderForCerebellum 渲染小脑可读的简化上下文
func (m *ContextManager) RenderForCerebellum(ctx context.Context, ac *AssembledContext) *CerebellumContext

// RenderForDiencephalon 渲染间脑中继用最小上下文
func (m *ContextManager) RenderForDiencephalon(ctx context.Context, ac *AssembledContext) *DiencephalonContext

// ---- ContextBridge API ----

// Publish / Subscribe / PublishDirect / AddScopeACL

// ---- session.Manager API ----

// SaveSession / GetSession
```

---

## 十一、实施路径与完成状态

| Phase | 内容 | 依赖 | 状态 |
|-------|------|------|------|
| **P1** | 核心类型：ConcreteNode, ContextGraph, NodeType | 无 | ✅ **已完成** |
| **P2** | MemoryContextManager Tier 1~3 + L1 缓存 | P1 | ✅ **已完成** |
| **P3** | ContextManager STM + Pristine + Pipeline | P1 | ✅ **已完成** |
| **P4** | Pipeline 处理器链 (Masking → Degradation → Distillation → Truncation) | P3 | ✅ **已完成** |
| **P5** | ContextBridge + RenderFor* (Cerebellum/Diencephalon/Brain) | P3 | ✅ **已完成** |
| **P6** | Session 持久化 — SurrealDB Core 模式 | P2 | ✅ **已完成** |
| **P7** | 静态提示词前缀 + KV Cache 优化 | P2 | ✅ **已完成** |
| **P8** | 模式感知配置 (mode-aware STM config) | P3 | ✅ **已完成** |
| **P9** | LTM：Memorize/Recall + 向量存储 | P6 | ⏳ **待实现** (stub) |
| **P10** | 集成到 Diencephalon drive 循环 | 全部 | 🔄 **进行中** |
