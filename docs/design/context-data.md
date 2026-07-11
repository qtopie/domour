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

### 2.1 两层存储架构

```
┌─────────────────────────────────────────────────────┐
│ MemoryContextManager（跨所有 Agent 共享）              │
│                                                       │
│  Tier 1 → System Instruction                          │
│    ├── globalMemory（~/.domour/*.md）                  │
│    └── userProjectMemory（.domour/*.md）               │
│                                                       │
│  Tier 2 → 首条 user message                            │
│    ├── extensionMemory（IDE/扩展记忆）                  │
│    └── projectMemory（项目上下文文件）                  │
│                                                       │
│  Tier 3 → JIT 文件发现                                 │
│    └── discoverContext(accessedPath, trustedRoots)    │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│ ContextManager（每 Agent 实例独有）                   │
│                                                       │
│  Pristine Graph（不可变备份）                          │
│  ┌──────────────────────────────────────────────┐    │
│  │ Nodes: map[string]*ConcreteNode               │    │
│  │ Edges: map[string][]string                    │    │
│  │ 用途: 保留原始版本，用于回退与校准               │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  Working Buffer（可操作）                              │
│  ┌──────────────────────────────────────────────┐    │
│  │ STM 环状缓冲区（带保护区）                      │    │
│  │ ├── 保护区域: 最近 N 轮（不压缩）               │    │
│  │ ├── 可压缩区域: 更旧轮次（摘要/截断节点）        │    │
│  │ └── 本轮新增: 用户消息 + 中间状态 + 工具结果    │    │
│  └──────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────┘
```

### 2.2 核心节点类型

```go
type NodeType string

const (
    NodeUserPrompt    NodeType = "user_prompt"     // 用户输入
    NodeAgentThought  NodeType = "agent_thought"   // LLM 推理输出
    NodeToolCall      NodeType = "tool_call"       // 工具调用请求
    NodeToolResult    NodeType = "tool_result"     // 工具执行结果
    NodeSystemEvent   NodeType = "system_event"    // 系统事件（系统指令、配置变更）
    NodeIntent        NodeType = "intent"          // 意图识别结果
    NodeSnapshot      NodeType = "snapshot"        // 摘要合成节点
    NodeRollingSummary NodeType = "rolling_summary" // 滚动摘要节点
)
```

### 2.3 节点字段定义

```go
type ConcreteNode struct {
    ID           string            // 稳定哈希 ID（基于 Content + Type + TurnID）
    Type         NodeType          // 节点类型
    Role         string            // 仅 "user" | "model"（角色，与 LLM API 对齐）
    TurnID       string            // 所属轮次 ID
    Timestamp    time.Time         // 创建时间戳
    Payload      interface{}       // 包装 LLM Provider 原生 Content 对象（双向映射关键）
    Metadata     map[string]string // 扩展属性：来源、Token 数、安全等级、意图标签等
    ReplacesID   string            // 1:1 替换链（如 ToolMasking 替换原始工具结果）
    AbstractsIDs []string          // N:1 摘要链（被哪些节点摘要替代）
    TokenCount   int               // 预估 Token 数
    Version      int64             // 乐观锁版本号
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
      ├── NodeTruncation: 截断节点（>4000 tokens）
      └── StateSnapshot: 紧急 GC 摘要
      → Render: 按 turnId 聚合，按 role 交替重建 LLM Content[]
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

#### ⑤ 持久化阶段（Persist）

```
输入: 更新后的 Working Buffer
动作: 
  ├── SaveGraph → Dapr State Store / 本地文件（fallback）
  ├── EvictToLTM: 压缩可压缩区域中最旧的节点→转入 LTM
  └── 更新会话元数据（轮次计数、Token 累计用量）
输出: 持久化完成
```

- 支持断点续传：恢复时从 Pristine + Working 重建上下文
- 离线环境优先写本地文件，上线后增量同步

---

## 四、关键设计原则

| 原则 | 说明 |
|------|------|
| **双向映射** | Graph ↔ LLM Content[] 可无损互转，Payload 包装 Provider 原生对象 |
| **不可变备份** | Pristine Graph 保留原始版本，所有修改在 Working Buffer 上操作 |
| **保护优先** | 最近 N 轮 + 活跃任务始终跳过压缩，确保交互连续性 |
| **按需加载** | Tier 1/2 一次性加载，Tier 3 JIT 按需发现，LTM 按语义检索 |
| **角色对齐** | Role 仅 `user`/`model`，与 LLM API 对齐，工具调用为节点类型而非角色 |
