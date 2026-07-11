# Domour Agent 上下文模型设计

> Agent 上下文是指 Agent 在执行过程中能 "看到" 的全部信息，以及这些信息如何在多个子 Agent 之间流动。
> 本设计遵循"不复用 Dapr 已提供的能力"原则。

---

## 一、背景与问题

### 上下文工程的作用

在 Agent 开发中，上下文工程是决定 Agent 行为边界和协作效率的基础设施，其核心作用包括：

| 维度 | 说明 |
|------|------|
| **视野控制** | 定义 Agent 能"看到"什么、不能看到什么，避免信息过少（遗漏关键决策依据）或过多（Token 爆炸、注意力分散） |
| **通信协议** | 多 Agent 协作时，定义父子 Agent 间信息的传递、隔离与回写规则，确保子 Agent 私有推理不泄露，共享状态能同步 |
| **Token 经济** | LLM 上下文窗口是稀缺资源，通过压缩（摘要、截断、Masking）、优先级分层（保护最近轮次和活跃任务）、分级存储（STM + LTM）来实现高效利用 |
| **持久化方案** | 进程重启后 Agent 能否恢复状态、跨进程能否共享同一上下文，取决于上下文的序列化、存储与加载策略 |

**一句话总结**：上下文工程 = 视野控制 + 通信协议 + Token 经济 + 持久化方案，是 Agent 正确性、效率和可扩展性的基石。

### 当前架构的问题

| 问题 | 描述 |
|------|------|
| **扁平 State** | `State` 是 `map[string]interface{}` + `[]Event`，无结构化上下文表示 |
| **无持久化** | `MemoryStore` 仅在进程内内存中，进程重启丢失 |
| **无上下文隔离** | 子 Agent（Cerebellum 本地循环）与父 Agent 共享同一 State，无私有工作区 |
| **无 Token 管理** | 无压缩、无截断、无摘要策略，大上下文导致 LLM Token 溢出 |
| **无跨进程共享** | 同一 DomourHost 的多个进程不能访问同一上下文 |
| **Brain.Memorize/Recall** | 接口已定义但未实现 |

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

## 二、三层上下文模型

```
┌───────────────────────────────────────────────────┐
│  GlobalContext                                     │
│  作用域: 同一 DomourHost 内所有 Agent               │
│  生命周期: 持久化，Host 启动时加载                   │
│  内容:                                              │
│  ├── 系统指令 (System Instruction)                  │
│  ├── ~/.domour/*.md 全局记忆文件                    │
│  └── 用户项目记忆 (.domour/*.md)                   │
├───────────────────────────────────────────────────┤
│  LocalContext                                      │
│  作用域: 同一 Session / AgentGroup                  │
│  生命周期: Session 生命周期                          │
│  内容:                                              │
│  ├── 当前会话的对话历史轮次                          │
│  ├── 任务链的中间状态（TaskStep 状态）               │
│  ├── 父 Agent 传递给子 Agent 的任务描述              │
│  └── 当前 AgentGroup 共享的工作上下文                │
├───────────────────────────────────────────────────┤
│  IsolatedContext                                   │
│  作用域: 单个 Agent 实例                             │
│  生命周期: Agent 实例生命周期                         │
│  内容:                                              │
│  ├── 子 Agent 私有的对话轮次和推理过程               │
│  ├── 子 Agent 的工具调用历史                         │
│  ├── 子 Agent 的反思/规划记录                        │
│  └── 不泄露给父 Agent 的内部状态                    │
└───────────────────────────────────────────────────┘
```

### 作用域隔离规则

| 操作 | Global | Local | Isolated |
|------|--------|-------|----------|
| 父 Agent 可读 | ✓ | ✓ | ✗ |
| 父 Agent 可写 | ✗ | ✓ | ✗ |
| 子 Agent 可读 | ✓ | ✓（按注入策略） | ✓ |
| 子 Agent 写回父 | ✗ | ✓（通过 InjectContext） | ✗ |

---

## 三、核心数据结构

### 3.1 ContextNode

最小上下文单元，基于内容哈希实现稳定标识：

```go
type ContextNodeType string

const (
    NodeUserPrompt    ContextNodeType = "user_prompt"
    NodeAgentThought  ContextNodeType = "agent_thought"
    NodeToolCall      ContextNodeType = "tool_call"
    NodeToolResult    ContextNodeType = "tool_result"
    NodeSystemEvent   ContextNodeType = "system_event"
    NodeSnapshot      ContextNodeType = "snapshot"       // 摘要合成节点
    NodeRollingSummary ContextNodeType = "rolling_summary" // 滚动摘要节点
)

type ContextScope string

const (
    ScopeGlobal   ContextScope = "global"
    ScopeLocal    ContextScope = "local"
    ScopeIsolated ContextScope = "isolated"
)

type ContextNode struct {
    ID           string            // 稳定哈希 ID（基于 Content + Type + Scope）
    Type         ContextNodeType
    Scope        ContextScope
    Timestamp    time.Time
    Role         string            // "user" | "assistant" | "tool" | "system"
    Content      string            // 文本内容
    Metadata     map[string]string // 来源、Token数、优先级等
    TurnID       string            // 所属轮次 ID
    ReplacesID   string            // 1:1 替换链（如 masking）
    AbstractsIDs []string          // N:1 摘要链（被哪些摘要节点替代）
    TokenCount   int               // 预估 Token 数
    Version      int64             // 乐观锁版本号
}
```

### 3.2 ContextGraph

上下文的有向无环图（DAG）表示：

```go
type ContextGraph struct {
    Nodes      map[string]*ContextNode  // nodeID → Node
    Edges      map[string][]string      // parentID → childIDs
    RootIDs    []string                 // 根节点（源头）
    Scope      ContextScope
    SessionID  string
    HostID     string
    Version    int64                    // 单调递增版本号
    UpdatedAt  time.Time
}
```

### 3.3 图操作 API

```go
type GraphBuilder interface {
    // AppendHistory 将新对话轮次追加到图中
    AppendHistory(ctx context.Context, graph *ContextGraph, turn *Turn) error

    // RemoveNode 移除指定节点（沿 edges 级联更新）
    RemoveNode(ctx context.Context, graph *ContextGraph, nodeID string) error

    // ReplaceNode 用新节点替换旧节点（维护 replacesId 链）
    ReplaceNode(ctx context.Context, graph *ContextGraph, oldID, newNode *ContextNode) error

    // AbstractNodes 将 N 个节点摘要为一个摘要节点
    AbstractNodes(ctx context.Context, graph *ContextGraph, nodeIDs []string, summary *ContextNode) error

    // Render 将图渲染为 LLM 可用的 Message 数组
    Render(graph *ContextGraph, sinceTurnID string) []*Message
}
```

---

## 四、ContextManager

### 4.1 职责

- 管理 ContextGraph 的生命周期（构建 → 工作缓冲区 → 管道处理 → 渲染）
- 协调 Scope 路由（按作用域分发和隔离）
- 对接 Dapr State Store 做持久化

### 4.2 接口

```go
type ContextManager struct {
    diencephalon    *DiencephalonNode
    daprClient      *dapr.Client
    storeName       string          // Dapr state store 名称

    buffer          *ContextWorkingBuffer  // 工作缓冲区
    orchestrator    *PipelineOrchestrator  // 管道调度器
}

// GetGraph 加载指定作用域的上下文图
func (m *ContextManager) GetGraph(ctx context.Context, sessionID string, scope ContextScope) (*ContextGraph, error)

// SaveGraph 持久化上下文图到 Dapr State Store
func (m *ContextManager) SaveGraph(ctx context.Context, graph *ContextGraph) error

// InjectContext 从 sourceScopes 注入上下文到 targetScope
// 用于父 Agent 创建子 Agent 时传递上下文
func (m *ContextManager) InjectContext(
    ctx context.Context,
    sourceSessions []string,
    sourceScopes []ContextScope,
    targetSession string,
    targetScope ContextScope,
    policy ContextPassingPolicy,
) error

// RenderHistory 将图渲染为 LLM 可用的消息数组
func (m *ContextManager) RenderHistory(
    ctx context.Context,
    sessionID string,
    scope ContextScope,
    lastNTurns int,
) ([]*Message, error)
```

### 4.3 Dapr 存储 Key 规则

```
context:{hostID}:global                              # Global 图
context:{hostID}:session:{sessionID}:local            # Local 图
context:{hostID}:session:{sessionID}:agent:{agentID}:isolated  # Isolated 图
memory:{sessionID}:{nodeID}                          # LTM 记忆节点
```

---

## 五、上下文管道处理器

### 5.1 处理器接口

```go
type PipelineProcessor interface {
    Name() string
    Process(ctx context.Context, buffer *ContextWorkingBuffer) error
}
```

### 5.2 处理器列表

| 处理器 | 触发条件 | 功能 | 配置项 |
|--------|----------|------|--------|
| **ToolMasking** | 每次新消息 | 对超长工具输出（>8000 tokens）做 masking，用摘要代替原始内容 | maxTokenThreshold |
| **BlobDegradation** | 每次新消息 | 降级大二进制数据（图片 base64 等），替换为占位描述 | maxBlobKb |
| **NodeDistillation** | Token 超限 | 蒸馏 >15000 tokens 的大节点，提取关键信息 | distillationThreshold |
| **NodeTruncation** | Token 超限 | 截断 >4000 tokens 的节点，保留前后文 | truncationThreshold |
| **StateSnapshot** | 紧急 GC | 对最旧的内容做 LLM 摘要，替换为 Snapshot 节点 | snapshotWindow |

### 5.3 触发机制

```
每次新消息加入
    │
    ├──→ evaluateTriggers(buffer, config)
    │        │
    │        ├── currentTokens > retainedTokens
    │        │     ├── 标记保护节点（最近 N 轮 + 活跃任务）
    │        │     ├── 计算 targetDeficit
    │        │     └── deficit 超过 coalescingThreshold
    │        │           → emitConsolidationNeeded()
    │        │
    │        └── PipelineOrchestrator 执行相关管道
    │
    └──→ 渲染前最终处理
```

**保护策略**：

| 保护类型 | 范围 | 原因 |
|----------|------|------|
| `recent_turn` | 最后完整一轮所有节点 | 保留最近交互上下文 |
| `active_task` | 正执行的工具调用节点 | 避免截断进行中的任务 |
| `system_instruction` | 系统指令节点 | 核心指令不被压缩 |

---

## 六、MemoryManager

### 6.1 接口

```go
type MemoryManager struct {
    stm *ShortTermMemory   // 短期记忆（ContextGraph 工作缓冲区）
    ltm *LongTermMemory    // 长期记忆（Dapr State Store 持久化）
}

// Memorize 将 ContextNode 存入长期记忆
func (m *MemoryManager) Memorize(ctx context.Context, nodes []*ContextNode) error

// Recall 从长期记忆中检索上下文
func (m *MemoryManager) Recall(ctx context.Context, query string, opts RecallOptions) ([]*ContextNode, error)

// Forget 删除指定记忆节点
func (m *MemoryManager) Forget(ctx context.Context, nodeIDs []string) error

// EvictFromContext 将过期的 STM 节点压缩后转入 LTM
func (m *MemoryManager) EvictToLTM(ctx context.Context, graph *ContextGraph, evictedIDs []string) error
```

### 6.2 STM：短期记忆窗口

```
┌─── 工作缓冲区 (ContextWorkingBuffer) ───┐
│  [Node-1] [Node-2] ... [Node-N]         │  ← 环状缓冲区
│                                         │
│  保护区域 (最近 N 轮, 不压缩)              │
│  ┌──────────────────────────────┐       │
│  │ Turn-K  │ Turn-(K+1) │ ...  │       │  ← 跳过管道处理
│  └──────────────────────────────┘       │
│                                         │
│  可压缩区域                              │
│  ┌──────────────────────────────┐       │
│  │ Turn-1 │ Turn-2 │ ... │ N   │       │  ← 按需蒸馏/截断/摘要
│  └──────────────────────────────┘       │
└─────────────────────────────────────────┘
```

### 6.3 LTM：长期记忆

LTM 通过 Dapr State Store 持久化，可选通过 Dapr Vector Store 做语义检索：

```yaml
# components/vectorstore.yaml (可选)
apiVersion: dapr.io/v1alpha1
kind: Component
metadata:
  name: memorystore
spec:
  type: vectorstores.chroma
  version: v1
  metadata:
  - name: serverAddress
    value: localhost:8000
```

**LTM 摘要策略**：

| 策略 | 描述 | 适用场景 |
|------|------|----------|
| **RollingSummary** | LLM 生成滚动摘要，每次新摘要基于前一次摘要 + 新内容 | 长对话持久化 |
| **SimpleTruncation** | 保留首尾中间截断，标注 `[truncated N tokens]` | 低资源场景 |
| **SemanticChunk** | 按语义边界切分 + 向量化 → Dapr Vector Store | 需要检索的场景 |

---

## 七、子 Agent 上下文透传

### 7.1 传递机制

```
父 Agent (Cerebrum 或 Cerebellum)
  │
  ├── 1. 决定创建子 Agent
  │
  ├── 2. ContextManager.InjectContext()
  │     ├── Scope: Global   → 子 Agent 可直接引用
  │     ├── Scope: Local    → 按 policy 过滤后注入
  │     └── Scope: Isolated → 不注入（子 Agent 自建）
  │
  ├── 3. 创建子 Agent
  │     ├── 新 SessionID / AgentID
  │     ├── 新的 Isolated 图（空）
  │     ├── 引用 Global 图（只读）
  │     └── 注入的 Local 图片段（写入后同步回父）
  │
  └── 4. 子 Agent 执行完毕
        └── Local 图变更同步回父 Agent 的 Local 图
```

### 7.2 注入策略

```go
type ContextPassingPolicy struct {
    IncludeGlobal   bool      // 是否注入全局上下文
    IncludeLocal    bool      // 是否注入局部上下文
    MaxTokens       int       // 注入上下文最大 Token 数（0=不限）
    ExcludeNodeTypes []ContextNodeType // 排除的节点类型
    IncludeTags     []string  // 只包含特定标签的节点
    Summarize       bool      // 是否先摘要再传递
    SummaryProvider string    // 摘要用模型 Provider ID
}

// 预定义策略
var (
    FullContext      = ContextPassingPolicy{IncludeGlobal: true, IncludeLocal: true}
    SummaryOnly      = ContextPassingPolicy{IncludeGlobal: true, IncludeLocal: true, MaxTokens: 2000, Summarize: true}
    GlobalOnly       = ContextPassingPolicy{IncludeGlobal: true, IncludeLocal: false}
    TaskDescription  = ContextPassingPolicy{IncludeGlobal: false, IncludeLocal: true, MaxTokens: 1000}
)
```

---

## 八、集成到现有 Domour 架构

### 8.1 组件定位

```
Existing Domour Components          Context Components
─────────────────────               ──────────────────
Diencephalon (感官中继)        ←→    ContextManager (上下文中继)
Cerebrum (认知推理)           ←→    MemoryManager (记忆存取)
Cerebellum (战术编排)         ←→    GraphBuilder (图构建)
Brainstem (运动执行)          ←→    Dapr State Store (持久化)
Engine (运行时编排)           ←→    ContextManager (初始化 + 注入)
```

### 8.2 数据流

```
                   Dapr State Store
                   ┌──────────┐
                   │ context* │ ←── 持久化/加载
                   │ memory*  │      │
                   └──────────┘      │
                                     │
                   ┌─────────────────┴──────────────────┐
                   │         ContextManager              │
                   │  ┌──────────────┬─────────────┐    │
                   │  │ GraphBuilder │ Orchestrator│    │
                   │  └──────┬───────┴──────┬──────┘    │
                   └─────────┼──────────────┼───────────┘
                             │              │
              ┌──────────────┼───── Inject ─┼──────────────┐
              │              │              │              │
         ┌────┴────┐   ┌────┴────┐   ┌─────┴─────┐  ┌────┴────┐
         │Global   │   │Local    │   │ Isolated  │  │其他Host │
         │Context  │   │Context  │   │ Context   │  │(Dapr    │
         │(只读)   │   │(可写)   │   │(私有)     │  │ Pub/Sub)│
         └─────────┘   └─────────┘   └───────────┘  └─────────┘
```

### 8.3 Brain 接口更新

```go
// 扩展已有 Brain 接口
type Brain interface {
    Think(ctx context.Context, observation Observation) (Intent, error)
    Memorize(ctx context.Context, info MemoryPayload) error    // 已定义，待实现
    Recall(ctx context.Context, query string) ([]MemoryPayload, error) // 已定义，待实现

    // 新增：绑定 ContextManager
    SetContextManager(m *ContextManager)
}
```

### 8.4 Engine 集成

```go
// coreEngine 新增字段
type coreEngine struct {
    // ... 现有字段
    contextManager *ContextManager    // 上下文管理器
    memoryManager  *MemoryManager     // 记忆管理器
}

// Start 时初始化
func (e *coreEngine) Start(ctx context.Context) error {
    e.contextManager = NewContextManager(e.daprClient, "contextstore", e.diencephalon)
    e.memoryManager = NewMemoryManager(e.daprClient, "contextstore", e.contextManager)
    // ... 启动已有节点
}
```

---

## 九、API 设计（供 Diencephalon/Reasoner 调用）

```go
// ---- Context 操作 ----

// 在当前 Session 的 LocalContext 中追加内容
func AppendToLocalContext(ctx, sessionID string, turn *Turn) error

// 在当前 Agent 的 IsolatedContext 中追加内容
func AppendToIsolatedContext(ctx, sessionID, agentID string, turn *Turn) error

// 将 LocalContext 同步到 GlobalContext（谨慎使用，需要高权限）
func PromoteToGlobal(ctx, sessionID string, nodeIDs []string) error

// ---- Memory 操作 ----

// 存入 LTM
func SaveToMemory(ctx, sessionID string, content string, tags []string) error

// 从 LTM 检索
func SearchMemory(ctx, query string, limit int) ([]*ContextNode, error)

// ---- 子 Agent 上下文管理 ----

// 创建子 Agent 时注入上下文
func InjectContextToChild(
    ctx,
    parentSessionID string,
    parentScope ContextScope,
    childSessionID string,
    childScope ContextScope,
    policy ContextPassingPolicy,
) error

// 子 Agent 完成时同步回父 Agent
func SyncBackToParent(
    ctx,
    childSessionID string,
    parentSessionID string,
) error
```

---

## 十、实施路径

| Phase | 内容 | 依赖 |
|-------|------|------|
| **P1** | 核心类型定义：ContextNode, ContextGraph, GraphBuilder | 无 |
| **P2** | ContextManager 基本 CRUD + Dapr State Store 集成 | P1 |
| **P3** | STM 工作缓冲区 + 环状队列 + 保护策略 | P2 |
| **P4** | LTM：Memorize/Recall + 摘要策略 | P2 |
| **P5** | Pipeline 处理器链：Masking → Distillation → Truncation → Snapshot | P3 |
| **P6** | 子 Agent 上下文注入：InjectContext + ContextPassingPolicy | P4, P5 |
| **P7** | 跨进程同步：Dapr Pub/Sub 事件广播 | P2 |
| **P8** | 集成到现有 Diencephalon drive 循环 + Reasoner 状态机 | 全部 |
