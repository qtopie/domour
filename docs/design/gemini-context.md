# Gemini CLI Agent 上下文设计分析

> 分析基于 `gemini-cli` 的 `packages/core/src` 源码。
> Agent 上下文是指 Agent 在执行过程中能 "看到" 的全部信息，以及这些信息如何在多个子 Agent 之间流动。

---

## 一、核心数据结构：Context Graph（上下文图）

整个上下文管理的核心是 **Episodic Context Graph（情节上下文图）**，一种基于 DAG 的中间表示。

```typescript
enum NodeType {
  USER_PROMPT,    // 用户输入
  SYSTEM_EVENT,   // 系统事件
  AGENT_THOUGHT,  // 模型思考
  TOOL_EXECUTION, // 工具调用/返回
  MASKED_TOOL,    // 被掩码的工具（敏感信息保护）
  AGENT_YIELD,    // Agent 让出控制权
  SNAPSHOT,       // 快照摘要（合成节点）
  ROLLING_SUMMARY,// 滚动摘要（合成节点）
}

interface ConcreteNode {
  id: string;                    // 稳定哈希 ID（基于 Part 内容）
  type: NodeType;
  timestamp: number;
  role: 'user' | 'model';
  payload: Part;                 // 直接包装 Gemini API 的 Part 对象
  turnId: string;                // 所属轮次 ID
  replacesId?: string;           // 1:1 替换链（如 masking）
  abstractsIds?: readonly string[]; // N:1 摘要链（如 summarization）
}
```

### 双向映射

| 方向 | 实现 | 说明 |
|------|------|------|
| **History → Graph** | `ContextGraphBuilder` (toGraph.ts) | 每个 `Part` 映射为一个 `ConcreteNode`，按 role 和 turnId 划分 |
| **Graph → History** | `fromGraph()` (fromGraph.ts) | 按 `turnId` 聚合，按 role 交替感知重建 `HistoryTurn[]` |

这种设计的核心优势：**对图进行操作（增删改节点）后，可以无损重建回 Gemini API 需要的 Content 数组**。

---

## 二、三层上下文管理架构

```
┌─────────────────────────────────────────────────────────────────┐
│  1. MemoryContextManager（跨所有 Agent 共享的文件层）            │
│     ├── 全局记忆 (global)    → System Instruction (Tier 1)       │
│     ├── 扩展记忆 (extension) → 首条 user message (Tier 2)        │
│     ├── 项目记忆 (project)   → 首条 user message (Tier 2)        │
│     ├── 用户项目记忆         → System Instruction (Tier 1)       │
│     └── JIT 上下文           → 按需文件发现 (Tier 3)              │
├─────────────────────────────────────────────────────────────────┤
│  2. ContextManager（Pipeline 引擎，管理图的生命周期）             │
│     ├── 原始图 (Pristine Graph): 不可变备份                      │
│     ├── 工作缓冲区 (Working Buffer): 处理器可增删改              │
│     ├── 管道处理器链: ToolMasking → BlobDegradation →           │
│     │   NodeDistillation → NodeTruncation → StateSnapshot        │
│     └── 触发器: new_message / retained_exceeded / gc_backstop    │
├─────────────────────────────────────────────────────────────────┤
│  3. GeminiChat（每个 Agent 实例拥有独立的聊天历史）              │
│     ├── AgentChatHistory: 记录该 Agent 的对话轮次                │
│     └── ChatRecordingService: 持久化 + 断点续传                   │
└─────────────────────────────────────────────────────────────────┘
```

### 第 1 层：MemoryContextManager（共享文件层）

所有 Agent（主 Agent + 子 Agent）共享同一个 `Config` 实例，也共享同一个 `MemoryContextManager`。

```typescript
class MemoryContextManager {
  private globalMemory: string;      // ~/.gemini/*.md
  private extensionMemory: string;   // IDE/扩展提供的记忆
  private projectMemory: string;     // 项目根目录 *.gemini/*.md
  private userProjectMemory: string; // 用户项目记忆目录

  getGlobalMemory(): string;         // Tier 1 → System Instruction
  getExtensionMemory(): string;      // Tier 2 → 首条用户消息
  getEnvironmentMemory(): string;    // Tier 2 → 首条用户消息
  getUserProjectMemory(): string;    // Tier 1 → System Instruction

  async discoverContext(accessedPath, trustedRoots): Promise<string>; // Tier 3 JIT
}
```

**记忆层级**（Tiered Context Model）:

| 层级 | 注入位置 | 内容 | 更新时机 |
|------|----------|------|----------|
| Tier 1 | System Instruction | global + userProjectMemory | 会话初始化 / refreshMcpContext |
| Tier 2 | 首条 user message | extension + project（含 MCP 指令） | 会话初始化 / refreshMcpContext |
| Tier 3 | JIT（消息中触发） | 从文件系统按路径发现 | 按需 |

### 第 2 层：ContextManager + Pipeline 引擎

```typescript
class ContextManager {
  private buffer: ContextWorkingBufferImpl; // 工作缓冲区
  private orchestrator: PipelineOrchestrator; // 管道调度器
  private historyObserver: HistoryObserver;   // 监听历史变更

  // 从 HistoryTurn 数组重建上下文给 LLM
  async renderHistory(
    pendingRequest?: HistoryTurn,
    activeTaskIds: Set<string>,
  ): Promise<{ history; apiHistory; didApplyManagement; baseUnits; processedNodes }>
}
```

**Pipeline 处理器链**（定义于 `profiles.ts`）:

| 管道名称 | 触发器 | 处理器 | 功能 |
|----------|--------|--------|------|
| Immediate Sanitization | `new_message` | ToolMasking | 对超长工具输出（>8000 tokens）进行 masking |
| | | BlobDegradation | 降级大二进制数据 |
| | | ImmediateNodeDistillation | 蒸馏>15000 tokens 的大节点 |
| Normalization | `retained_exceeded` | NodeDistillation | 蒸馏>3000 tokens 的节点 |
| | | NodeTruncation | 截断节点到 4000 tokens |
| Emergency Backstop | `gc_backstop` | StateSnapshot | 对最旧的内容做摘要压缩到 4000 tokens |
| Async Background GC | `nodes_aged_out` | StateSnapshotAsync | 异步累加摘要 |

**Pipeline 设计特点**：
- **事件驱动**：通过 `ContextEventBus` 的 `onChunkReceived` / `onConsolidationNeeded` 触发
- **Mutex 保护**：同名管道串行执行，避免竞态
- **Stale Result 丢弃**：如果处理器返回时目标节点已被其他处理器移除，则丢弃结果
- **Hysteresis 阈值**：token deficit 增长超过 coalescing threshold 才触发压缩，避免频繁调用

### 第 3 层：GeminiChat（独立聊天会话）

```typescript
class GeminiChat {
  agentHistory: AgentChatHistory;           // 聊天历史
  chatRecordingService: ChatRecordingService; // 持久化录音
  context: AgentLoopContext;                // 执行上下文

  async sendMessageStream(modelConfigKey, message, prompt_id, signal, role, displayContent);
  // → AsyncGenerator<StreamEvent>
}
```

---

## 三、子 Agent 之间的上下文传递机制

**核心设计哲学：子 Agent 之间不共享聊天历史。** 每个子 Agent 创建全新的 `GeminiChat` 实例。

### 3.1 上下文传递路径图

```
父 Agent (GeminiChat)
  │
  ├── AgentChatHistory (包含所有对话历史，由 ContextManager 管理)
  │
  ├── 调用 AgentTool (agent_name, prompt)
  │     │
  │     └── LocalAgentExecutor.create()
  │           │
  │           ├── 创建隔离的 ToolRegistry
  │           │     ├── 从父 ToolRegistry clone 所有工具
  │           │     ├── 排除 AgentTool（阻止递归调用子 Agent）
  │           │     └── 排除 UPDATE_TOPIC_TOOL
  │           │
  │           ├── 创建隔离的 PromptRegistry
  │           ├── 创建隔离的 ResourceRegistry
  │           │
  │           ├── messageBus ← parent.derive(subagentName)
  │           │     （带子 Agent 名称前缀，用于确认消息路由）
  │           │
  │           ├── config: 共享（指向同一 Config 实例）
  │           ├── geminiClient: 共享（同一 LLM 客户端）
  │           └── sandboxManager: 共享
  │
  └── LocalAgentExecutor.executionContext（AgentLoopContext 实现）
        │
        ├── config: shared (this.context.config)
        ├── promptId: UUID (agentId, 唯一)
        ├── parentSessionId: 保留父 session ID
        ├── geminiClient: shared
        ├── sandboxManager: shared
        ├── toolRegistry: 隔离副本
        ├── promptRegistry: 隔离副本
        ├── resourceRegistry: 隔离副本
        └── messageBus: derived
              │
              └── 创建新的 GeminiChat（全新 AgentChatHistory）
                    │
                    ├── SystemPrompt
                    │     ├── 来自 definition.promptConfig.systemPrompt
                    │     ├── 经过模板化（${param} 替换）
                    │     └── 没有继承父 Agent 的对话历史
                    │
                    └── First User Message
                          ├── environmentMemory (共享 Config.getSessionMemory())
                          ├── user hints (InjectionService)
                          └── query (模板化后的任务描述)
```

### 3.2 三条信息传递通道

| 通道 | 方向 | 机制 | 内容 |
|------|------|------|------|
| **Prompt 参数** | 父 → 子 | `agent_tool.prompt` 参数 | 任务描述、相关事实、上下文背景 |
| **共享 Memory** | 共享 → 子 | `Config.getSessionMemory()` | extension + project 记忆文件 |
| **InjectionService** | 用户 → 子 | 事件监听广播 | 用户操作指引 (`user_steering`) |

### 3.3 子 Agent 的独立环境

每个子 Agent 创建时：
1. **全新 `GeminiChat`**：`AgentChatHistory` 初始为空，仅包含 system prompt + first user message
2. **隔离的工具注册表**：父工具被 `clone()` 到子注册表（有自己的状态），AgentTool 被排除防止递归
3. **派生的 MessageBus**：`parentMessageBus.derive(name)` 确保确认消息能正确路由回子 Agent
4. **独立的 PromptRegistry**：MCP 提供的 prompts 按需注册
5. **独立的 ResourceRegistry**：MCP 提供的 resources 按需注册
6. **可选的扩展工作区**：`workspaceDirectories` 提供额外文件系统访问范围
7. **可选的 memoryInboxAccess**：控制是否能访问 auto-memory 的 patch 文件

### 3.4 子 Agent 的反馈通道（反向数据流）

```
子 Agent 执行中:
LocalAgentExecutor
  │
  ├── onActivity callback
  │     ├── SubagentActivityEvent.THOUGHT_CHUNK
  │     ├── SubagentActivityEvent.TOOL_CALL_START
  │     ├── SubagentActivityEvent.TOOL_CALL_END
  │     └── SubagentActivityEvent.ERROR
  │           │
  │           └── LocalSessionInvocation 转换为 SubagentProgress
  │                 ├── 包含最近 N 条活动 (MAX_RECENT_ACTIVITY=3)
  │                 └── 通过 updateOutput() 推送到 UI
  │
  └── OutputObject { result, terminate_reason, turn_count, duration_ms }
        │
        └── LocalSessionInvocation.execute() 构建 ToolResult
              │
              ├── llmContent: `Subagent 'X' finished.\nResult:\n${output.result}`
              │     └── 以 functionResponse 形式进入父 Agent 的 chat history
              │
              └── returnDisplay: SubagentProgress（UI 显示）
```

---

## 四、上下文压缩与 Token 管理

### 4.1 触发条件

```
ContextManager.evaluateTriggers(newNodes)
  │
  ├── 计算当前 buffer 的总 tokens
  │
  ├── if (currentTokens > retainedTokens) {
  │     ├── 标记溢出节点（保护最近一轮 + 活跃工具调用）
  │     ├── 计算 targetDeficit
  │     └── if (deficit 增长超过 coalescingThreshold) {
  │           emitConsolidationNeeded()
  │         }
  │   }
  │
  └── PipelineOrchestrator 执行相关管道
```

### 4.2 保护策略

| 保护类型 | 范围 | 原因 |
|----------|------|------|
| `recent_turn` | 最后完整一轮的所有节点 | 保留最近的交互上下文 |
| `external_active_task` | 外部指定的活跃任务节点 | 避免截断正在进行的任务 |

### 4.3 校准机制

- **Hot Start Calibration**：会话恢复时，首次调用 `countTokens` API 校准本地 token 估算
- **Token Calculator**：支持 `getRawBaseUnits`（快速估算）和 `calculateTokensAndBaseUnits`（精确计算）

---

## 五、关键设计决策总结

| 方面 | 设计选择 | 理由 |
|------|----------|------|
| **上下文隔离** | 子 Agent 独立 `GeminiChat`，不继承父历史 | 防止子 Agent 浪费 token 在无关历史；鼓励父 Agent 提炼关键信息传给子 Agent |
| **信息传递** | 通过 `prompt` 文本参数显式传递 | 迫使父 Agent 思考什么是子 Agent 需要的上下文 |
| **工具隔离** | 工具被 `clone()`，禁用 AgentTool | 防止递归调用；允许子 Agent 有独立的工具状态 |
| **记忆共享** | 共享 `Config.getSessionMemory()` | 避免重复加载记忆文件；确保一致性 |
| **上下文压缩** | Context Graph + Pipeline Processors | 相比简单 truncation 更精确；保留关键信息；支持摘要合成 |
| **结果返回** | 文本拼接进 ToolResult.llmContent | 保持协议简单；父 Agent 自行解析结果文本 |
| **实时反馈** | SubagentActivityEvent → SubagentProgress | 让用户在子 Agent 运行时能看到思考过程和工具调用 |

## 六、类比 Domour 架构

| Gemini CLI | Domour | 功能 |
|------------|--------|------|
| MemoryContextManager | Diencephalon（感官中继） | 加载和组织外部记忆 |
| Context Graph + Pipeline | Cerebellum（逻辑编排） | 上下文管理、压缩、重建 |
| GeminiChat / AgentExecutor | Cerebrum（认知推理） | 与 LLM 交互、执行工具 |
| AgentTool + Subagent Protocol | Brainstem（运动层） | 子 Agent 的创建、通信、安全拦截 |
| SubagentActivityEvent | 事件总线 | Agent 间实时状态反馈 |
