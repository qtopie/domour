# domour 设计文档

本文档定义了 `domour` 作为 **Agent Runtime** 的核心设计理念、架构要素、概要设计及演进方向。

---

## 1. 脑组件与协调

脑组件架构与信号协作内容已移至 `docs/brain/brain-components-coordination.md`。

该文件包含：

- 仿生脑组件层级（大脑、小脑、脑干、间脑）。
- 运行时核心节点定义与信号通路设计。
- 组件协作与脑桥旁路监听的调度规则。

---



## 2. 插件化与能力扩展

Runtime 必须具备极强的扩展性，以适配不同的业务场景：

- **能力注册 (Capability)**: 插件可以注册 UI 渲染、规划、自省、向量检索等能力。
- **Skill 系统**: 支持通过 `SKILL.md` (Markdown) 定义 Agent 技能，将描述性约束与代码逻辑解耦。
- **大模型动态路由**: 依托间脑 (Diencephalon) 实现底层 LLM Provider 的按需平滑切换。

---

## 3. 自省能力 (Reflection)

作为 Runtime，`domour` 提供了内置的自省闭环：
- **过程自省 (ReAct)**: 小脑实时监控工具执行反馈，本地动态调整或回滚战术动作。
- **认知自检 (Verification)**: 大脑在做出重大决策或输出最终答复前，进行一致性自检或依赖沙箱测试验证结果。

---

## 4. 统一会话与记忆管理

Runtime 负责管理智能体的“灵魂碎片”：
- **延迟加载 (Lazy-Load)**: 能够自动从本地 CLI 日志中恢复会话并同步至中心数据库。
- **多源归并**: 将云端存储与本地文件系统中的会话记录统一为单一的时间线视图。
- **自动压缩**: 当上下文达到阈值时，自动触发由 Brain 执行的摘要压缩逻辑。

---

## 5. 分布式协同与集群愿景

`domour` 不仅是单机运行时，更是智能体集群的基座：
- **节点路由**: 任务可以在不同的节点（如云端 Brain 节点、边缘 Motor 节点）之间自由流转。
- **分布式流式转发 (Relay)**: 基于专用 gRPC Relay 实现高效的跨节点流式事件传输。
- **安全沙箱**: 通过物理隔离 Brain 与 Motor 节点，构建零信任的 Agent 运行环境。

---

## 6. 系统状态与运行模式

`domour` 通过显式的状态机和运行模式管理，确保在不同环境（云、边、端）下的能效比与功能对齐。在高层 API 中，这些能力通过 `ark/governor` 包暴露。

### 6.1 任务生命周期 (Task Lifecycle)
任务在脑组件中遵循严格的状态流转：
- **Pending**: 任务已创建，等待调度。
- **Running**: 任务正在由小脑或执行器处理。
- **Completed**: 任务执行成功，已获得观测结果 (Observation)。
- **Failed**: 任务执行失败。

### 6.2 脑状态 (State)
大脑维护一个全局上下文看板，包含：
- **SessionID**: 唯一会话标识。
- **GlobalGoal**: 最终目标。
- **Complexity**: 复杂度评分 (1-10)，决定执行路径（如 Simple 对应直接执行，Complex 对应 Planner/Worker 编排）。
- **Steps**: 原子任务步骤序列。
- **CurrentStepID**: 当前执行中的步骤指针。
- **UserFeedback**: 执行过程中捕获的用户即时指令。

### 6.3 运行模式 (System Modes)
系统根据“认知能效 (Cognitive Power)”与“仿生能效 (Bionic Power)”的平衡，定义了多种运行模式：

| 模式 | 认知能效 (LLM) | 仿生能效 (I/O) | 场景描述 |
| :--- | :--- | :--- | :--- |
| **Hibernate (休眠)** | Off | Off | 完全停机，零能耗。 |
| **Casual (日常)** | Low | Low | 维持心跳，低频响应。 |
| **Balanced (平衡)** | Normal | Normal | 标准交互，体验与功耗的最佳平衡。 |
| **Performance (性能)** | High | High | 最大并发，开启并行执行与 io_uring。 |
| **Vigilant (警觉)** | Low | High | 脑部悬置，仿生层高敏感，响应反射弧。 |
| **Survival (生存)** | Local | Normal | 离线自治，回退至本地小模型。 |
| **Deep Think (深思)** | High | Off | 物理静止，全力投入长链推理与知识重构。 |
| **Stealth (隐匿)** | Normal | Encrypted | 切断遥测，严格的 I/O 去敏感与加密流。 |
| **Diagnostic (诊断)** | Normal | Sandbox | 沙箱环境，物理输出被拦截或重定向。 |

---

## 7. 核心模块与暴露的能力设计 (Modular Capabilities)

`domour` 提供的 `ark` (Agent Runtime Kit) SDK 被拆解为 9 个核心模块。这些模块不仅在内部高度解耦，同时也会根据需要向开发者暴露相应的 API：

### 7.1 推理引擎 (Reasoning Engine)
负责 Agent 的“思考”过程，将任务转化为具体的动作。
* **模型适配与路由 (Model Adapters & Routing)**：统一接入 Gemini, DeepSeek 等模型接口，支持回退机制（Fallback）。
* **思维范式 (Reasoning Paradigms)**：内置 ReAct, Plan-and-Solve 等范式。
* **提示词管理 (Prompt Management)**：系统提示词的动态注入与模板渲染。

### 7.2 记忆系统 (Memory System)
Agent 的上下文与经验管理。
* **短期记忆 (Short-term / Working Memory)**：当前 Session 的多轮对话上下文管理，包括自动截断与滑动窗口。
* **长期记忆 (Long-term Memory)**：事实提取与向量化存储，支持通过 RAG 检索历史经验。
* **记忆抽象接口**：对外暴露 `MemoryBackend` 接口，允许开发者挂载不同的存储实现。

### 7.3 编排 (Orchestration)
负责任务生命周期与多 Agent 协同。
* **任务调度 (Task Scheduling)**：管理 Agent 状态（启动、休眠、唤醒、停止）。
* **工作流编排 (Workflow Orchestration)**：支持将复杂任务拆解为子任务（Sub-tasks），并交由不同的 Agent 或 Worker 执行。
* **事件总线 (Event Bus)**：基于发布/订阅模型的内部事件分发。

### 7.4 工具 (Tools)
让 Agent 具备与物理世界交互的能力。
* **工具注册中心 (Tool Registry)**：通过强类型的 Go 接口注册本地函数作为工具。
* **工具调用协议 (Tool Call Protocol)**：标准化大模型的 Function Calling 输出，转化为 Go 的函数调用并捕获执行结果。
* **技能组管理 (Skill Groups)**：支持按领域（如：Desktop Control, File System）挂载工具集合。

### 7.5 质量评估 (Quality Evaluation)
保障 Agent 输出结果的可靠性。
* **幻觉检测 (Hallucination Detection)**：在返回结果前进行自我一致性检查或事实核查。
* **护栏策略 (Guardrails)**：定义输出格式校验规则，确保输出的 JSON 或数据结构符合预期。
* **结果自省 (Self-Reflection)**：当执行失败时，由引擎触发自动反思并重试。

### 7.6 认证授权 (Authentication & Authorization)
多租户和多用户环境下的身份标识。
* **Session 鉴权 (Session Auth)**：验证当前会话的合法性，绑定对应的 User ID 和 Tenant ID。
* **访问控制 (Access Control)**：基于角色的权限控制 (RBAC)，限制特定 Agent 访问敏感服务或数据。

### 7.7 安全与隐私 (Security & Privacy)
将非确定性的模型输出关在笼子里。
* **零信任沙箱 (Zero-Trust Execution)**：工具执行的环境隔离与超时控制。
* **操作审批/否决 (Veto System)**：危险指令（如文件删除、系统命令）的自动拦截与人工确认（Human-in-the-loop）机制。
* **数据脱敏 (Data Masking/PII Redaction)**：在将上下文发送给大模型前，自动擦除敏感信息。

### 7.8 可观测性 (Observability)
让 Agent 的黑盒过程白盒化。
* **思维轨迹追踪 (Traceability)**：使用 OpenTelemetry 记录每一步的思考链路、耗时和工具调用明细。
* **运行指标监控 (Metrics)**：监控 Token 消耗速率、工具调用成功率、任务执行耗时等。
* **事件钩子 (Event Hooks)**：对外暴露 `OnThink`, `OnAct`, `OnError` 等回调，方便业务层实现 UI 状态同步。

### 7.9 基础设施 (Infrastructure)
底层的支撑组件，为上述模块提供物理存储。
* **数据库 (DB)**：默认内置轻量级数据库（SQLite 适用于嵌入式/边缘计算，BadgerDB 适用于桌面端），存储 Session、配置和任务元数据。
* **缓存 (Cache)**：提供多级缓存（L1 内存缓存 / L2 磁盘缓存），用于缓存高频提示词或重复查询的结果。
* **文件存储 (Storage)**：管理 Agent 执行过程中的产物（Artifacts），如生成的文档、日志、图片等。

---

## 8. 下一步演进

1. **核心模块重构落地**: 基于上述 9 大模块，梳理 `ark` 库的代码结构，剥离复杂的内部仿生概念。
2. **Runtime 接口标准化**: 进一步抽象 `CognitorClient` 和 `ExecutorClient` 的远程协议。
3. **多智能体协同协议**: 定义节点间基于意图的交互协议，实现真正的“蜂群”协同。
