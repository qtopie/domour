# Domour Agent 上下文数据设计

> 本文档定义 Agent 上下文的**数据来源、数据结构与数据流转**。
> 基于 `docs/design/agent-context.md` 的总体架构，此处聚焦数据面的具体设计。

---

## 一、数据来源

Agent 每轮可以感知到以下 6 类数据，按来源层级划分：

| # | 来源 | 层级 | 注入时机 | 说明 |
|---|------|------|---------|------|
| 1 | **系统全局提示词** | Tier 1 → System Instruction | 会话初始化 | 角色定义、全局行为规则、Skills/工具元数据 |
| 2 | **用户消息** | 本轮输入 | 每轮 | 用户 Prompt、附件文件、内联指令 |
| 3 | **意图识别结果** | 本轮中间状态 | Diencephalon 解析后 | 识别到的意图标签、提取的实体、路由目标 |
| 4 | **历史消息（STM 窗口）** | Working Buffer | 每轮组装 | 最近 N 轮完整对话 + 更旧轮次的摘要/截断节点 |
| 5 | **长短期记忆** | Tier 3 / LTM | 按需 Recall | 全局记忆文件、跨会话持久化知识、语义检索结果 |
| 6 | **会话元数据** | 内建 | 始终可用 | SessionID、AgentID、当前运行模式、Token 用量累计 |

---

## 二、数据结构

### 2.1 两层存储架构（代码实现）

```
┌─────────────────────────────────────────────────────┐
│ MemoryContextManager（全局共享 — 仅一个实例）          │
│                                                       │
│  Tier 1 → System Instruction (纯静态, L1 30min TTL)   │
│    ├── 全局规则 ~/.domour/*.md (+ Skills 元数据)      │
│    └── 项目记忆 .domour/*.md                          │
│                                                       │
│  Tier 2 → 同 Tier 1（与 Tier1 合并到 system message）  │
│    └── 区别仅在于来源路径不同，渲染时合并              │
│                                                       │
│  Tier 3 → JIT 文件发现 (L1 5min TTL)                  │
│    └── discoverContext(ctx, accessedPath)             │
│    └── 响应式: 用户在 IDE 中跳转/打开文件时触发       │
└──────────────────────┬──────────────────────────────┘
                       │ 共享 Config + L1 Cache
┌──────────────────────▼──────────────────────────────┐
│ ContextManager（每 Agent 实例独有）                   │
│                                                       │
│  Pristine Graph（不可变备份）                          │
│  ┌──────────────────────────────────────────────┐    │
│  │ Nodes: map[string]*ConcreteNode               │    │
│  │ Edges: map[string][]string                    │    │
│  │ 用途: "真实发生了什么" 的原始记录               │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  STM 环状缓冲区（带保护区）                            │
│  ┌──────────────────────────────────────────────┐    │
│  │ 保护区域: 最近 N 轮（跳过 Pipeline 处理）       │    │
│  │ 可压缩区域: 更旧轮次（蒸馏/截断/摘要）           │    │
│  │ 本轮新增: 用户消息 + 工具结果                   │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  Pipeline 处理器链 (mode 感知)                        │
│  ┌──────────────────────────────────────────────┐    │
│  │ ToolMasking → BlobDegradation →              │    │
│  │ NodeDistillation → NodeTruncation            │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  ContextBridge (跨 Agent Channel Pub/Sub)             │
└─────────────────────────────────────────────────────┘
```

### 2.2 核心节点类型（代码实现）

```go
type NodeType string

const (
    NodeUnknown        NodeType = "unknown"
    NodeInstruction    NodeType = "instruction"     // 系统指令/提示词
    NodeConversation   NodeType = "conversation"    // 对话轮次
    NodeObservation    NodeType = "observation"     // 观察/感知输入
    NodeAction         NodeType = "action"          // 动作/行动
    NodePlan           NodeType = "plan"            // 规划结果
    NodeReflection     NodeType = "reflection"      // 反思/自省
    NodeSummary        NodeType = "summary"         // 摘要
    NodeSnapshot       NodeType = "snapshot"        // 摘要合成节点
    NodeToolResult     NodeType = "tool_result"     // 工具执行结果
)
```

### 2.3 节点字段定义（代码实现）

```go
type ConcreteNode struct {
    ID        string            `msgpack:"id"`         // 唯一标识
    Type      NodeType          `msgpack:"type"`       // 节点类型
    AgentID   string            `msgpack:"agent_id"`   // 所属 Agent
    SessionID string            `msgpack:"session_id"` // 所属会话
    Scope     ContextScope      `msgpack:"scope"`      // 作用域
    Turn      int               `msgpack:"turn"`       // 所属轮次索引
    Priority  int               `msgpack:"priority"`   // 优先级 0~100
    TokenSize int               `msgpack:"token_size"` // 预估 Token 数
    Content   string            `msgpack:"content"`    // 文本内容
    Metadata  map[string]string `msgpack:"metadata"`   // 扩展属性
    Tags      []string          `msgpack:"tags"`       // 标签索引
    CreatedAt int64             `msgpack:"created_at"` // 创建时间戳 (unix)
    children  []*ConcreteNode   // Graph 内部子节点（不序列化）
}
```

### 2.4 历史消息窗口结构

STM 环状缓冲区维护了一个**带保护区的滑动窗口**：

```
每次新轮次追加后：
┌─────────────────────────────────────────────────────┐
│  保护区域（最近 N 轮，跳过 Pipeline 处理）            │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐              │
│  │Turn-k│ │Turn-..│ │Turn-..│ │Turn-N│              │
│  └──────┘ └──────┘ └──────┘ └──────┘              │
├─────────────────────────────────────────────────────┤
│  可压缩区域（更旧的轮次，按需处理）                    │
│  ┌──────┐ ┌──────┐ ┌──────────────────────────┐    │
│  │Turn-1│ │Turn-2│ │... (Snapshot/RollingSummary) │  │
│  └──────┘ └──────┘ └──────────────────────────┘    │
├─────────────────────────────────────────────────────┤
│  JIT 注入（Tier 3）：按需发现的文件/记忆            │
│  本轮新增：用户消息、意图识别、工具结果              │
└─────────────────────────────────────────────────────┘
```

窗口行为规则：
- 新轮次加入时，保护区域中最旧的一轮移出保护区域
- 移出的轮次进入可压缩区域，触发 Pipeline 评估
- 被压缩的轮次替换为 `Snapshot` 或 `RollingSummary` 节点

---

## 三、数据流转（单轮 5 阶段）

### 阶段总览

```
 ① 组装 ────→ ② 管道 ────→ ③ 推理 ────→ ④ 回写 ────→ ⑤ 持久化
```

### 阶段详解

#### ① 组装阶段（Assembly）

```
输入: 6 类数据源
动作: MemoryContextManager 加载 Tier 1/2 → 构建 System Instruction
      Working Buffer 加载 STM 窗口历史 + 本轮用户输入
      LTM Recall（按需）→ 注入相关记忆
      Diencephalon 意图识别 → 注入 Intent 节点
输出: 填充完毕的 Working Buffer
```

- Tier 1/2 仅在会话初始化时加载，后续轮次复用
- 用户消息每轮作为新的 `user_prompt` 节点加入

#### ② 管道阶段（Pipeline）

```
输入: Working Buffer（含本轮新增）
动作: PipelineOrchestrator 评估 Token 阈值
      ├── ToolMasking: 掩码超长工具输出（>8000 tokens）
      ├── BlobDegradation: 降级大二进制数据
      ├── NodeDistillation: 蒸馏大节点（>15000 tokens）
      └── NodeTruncation: 截断节点（>4000 tokens）
      → Render: 按 turn 聚合，重建 LLM Content[]
输出: LLM 可消费的 Content 消息数组
```

- 保护区域（最近 N 轮）跳过所有处理器
- 单轮场景通常只触发 ToolMasking

#### ③ 推理阶段（Inference）

```
输入: LLM Content[]
动作: Diencephalon.SendToLLM(Content[], ModelConfig)
      ├── LLM 返回 Response（文本 / ToolCall）
      ├── ToolCall → Brainstem 执行 → 结果回写 Working Buffer
      └── 可能有多次 ToolCall 循环
输出: LLM Response（最终文本 + 工具执行结果）
```

- ToolCall 循环期间，中间结果实时写入 Working Buffer
- 每个 ToolCall 执行结果作为一个 `tool_result` 节点

#### ④ 回写阶段（Writeback）

```
输入: LLM Response
动作: 追加到 Working Buffer
      ├── AGENT_THOUGHT 节点（LLM 推理文本）
      ├── TOOL_CALL 节点（工具调用请求）
      └── TOOL_RESULT 节点（工具执行结果）
输出: 更新后的 Working Buffer（Pristine Graph 不变）
```

- Pristine Graph 始终不可变，用于对比和回退
- 本轮所有节点关联同一 TurnID

#### ⑤ 持久化阶段（Persist — 仅 Core 模式）

```
输入: 更新后的 Working Buffer
动作: 
  ├── session.Manager.SaveSession → SurrealDB (Upsert + L1/L2 缓存)
  │     ├── 三层: L1 (Otter进程内) → L2 (SurrealDB缓存表, 24h TTL) → DB (SurrealDB会话表)
  │     └── RecordID 处理连字符 (models.NewRecordID)
  ├── EvictToLTM: 压缩可压缩区域中最旧的节点→转入 LTM（stub）
  └── 更新会话元数据（轮次计数、Token 累计用量）
输出: 持久化完成
```

- Core 模式: SurrealDB 持久化，重启恢复
- Standalone 模式: MemoryStore（进程内）或 BadgerStore（磁盘文件）
- 所有模式均支持从 L1 → L2 → DB 三级回退恢复

---

## 四、关键设计原则

| 原则 | 说明 |
|------|------|
| **双向映射** | Graph ↔ LLM Content[] 可无损互转，Payload 包装 Provider 原生对象 |
| **不可变备份** | Pristine Graph 保留原始版本，所有修改在 Working Buffer 上操作 |
| **保护优先** | 最近 N 轮 + 活跃任务始终跳过压缩，确保交互连续性 |
| **按需加载** | Tier 1/2 一次性加载，Tier 3 JIT 按需发现，LTM 按语义检索 |
| **角色对齐** | Role 仅 `user`/`model`，与 LLM API 对齐，工具调用为节点类型而非角色 |
