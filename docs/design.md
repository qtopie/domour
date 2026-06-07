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

## 7. 下一步演进

1. **Runtime 接口标准化**: 进一步抽象 `CognitorClient` 和 `ExecutorClient` 的远程协议。
2. **轻量化脑干**: 优化在嵌入式设备上的资源占用。
3. **多智能体协同协议**: 定义节点间基于意图的交互协议，实现真正的“蜂群”协同。
