# domour 架构重构经验与设计规范 (Architectural Lessons Learned)

本文档记录了 `domour` 在重构过程中的核心经验教训、架构演进抉择及开闭原则（OCP）落地规范。

---

## 1. 对齐 Go 惯例与 adk-go 模式 (Sub-packages SDK Isolation)

### 经验教训
在早期设计中，`ark/` 作为对外暴露的 SDK，直接依赖或泄漏了 `internal/brain`、`*tool.Manager` 等底层具体实现类型。这破坏了包的隔离性，导致外部使用者必须理解内部实现细节。

### 规范定型
- **多子包解耦 (Sub-packages)**: 对外暴露按领域划分的精简公开包（如 `ark/agent`、`ark/cognitor`、`ark/governor`、`ark/hub`、`ark/orchestrator`、`ark/session`、`ark/telemetry`）。
- **零内部类型泄漏 (Zero `internal/` Leakage)**: `ark/` 目录下的公开 API 签名不得出现任何 `internal/...` 包中的类型。
- **工厂函数与轻量 Struct**: 通过轻量 interface 与结构体 + 工厂函数（`NewLLMAgent`, `NewArkHub`）屏蔽构建细节。

---

## 2. 隐藏 Orchestration 细节与 `internal/engine` 职责收拢

### 经验教训
此前 `internal/app/assistant` (gRPC/HTTP 协议驱动层) 手动初始化了 `localbus.NewEventBus()` 和 `localorch.NewLocalOrchestrator(eng, eb)` 并将其硬编码注入到 `AssistantService` 中。这导致驱动层暴露了 Agent 执行回路 (ReAct Loop) 与编排器细节。

### 规范定型
- **Engine 作为唯一编排与调度中心 (`internal/engine/orchestrator.go` & `local_orchestrator.go`)**: 
  - `AgentOrchestrator` 接口归位于 `internal/engine/orchestrator.go`；默认内存编排器归位于 `internal/engine/local_orchestrator.go`；
  - 彻底删除了已无任何依赖的冗余包 `internal/infra/dapr/local`。
- **Cognitor 认知网关收拢 Think 标签解析器**: `<think>...</think>` 流式解析器归位于 `internal/cognitor/proxy/think_parser.go`，由 LLM / Cognitor 代理统一负责模型输出的流式解析。
- **多 Reasoning 机制解耦**: 编排器不应限定或硬编码为唯一的 ReAct 循环，而是通过 `runReasoningLoop` 分发支持多种推理范式（`ReAct` 战术循环、`Simple` 单轮生成、`Planner` 规划循环等）。
- **驱动层极简依赖**: `internal/app/assistant` 仅需感知 `engine.Engine` 与 `session.Store`，初始化签名简化为 `NewAssistantService(eng, store)`。
- **开闭原则 (OCP)**: 扩展新的 Reasoning 模式（如 Reflexion, Tree of Thoughts）或分布式 Workflow 模式无需修改应用驱动层。

---

## 3. 消费端接口定义与循环依赖规避 (Interface Segregation)

### 经验教训
在收拢 `Orchestrator` 到 `internal/engine` 时，若 `internal/infra/dapr/local` 导入 `internal/engine.Engine`，而 `internal/engine` 又导入 `internal/infra/dapr/local`，会导致 Go 语言典型的 **Import Cycle** (循环依赖)。

### 规范定型
- **消费端定义接口 (Consumer-side Interface)**: 基础设施层（如 `internal/infra/dapr/local`）只需在本地定义其所依赖的最小接口（如 `CognitorClient`、`ExecutorClient`），而不是直接导入上层 `internal/engine`。
- **彻底解耦**: 彻底打破上层 coordinator 与底层 infra 之间的双向依赖，同时保持 100% 编译期类型安全与易测试性。

---

## 4. `internal/brain` 的纯粹仿生与 Agent 类型无关性

### 经验教训
大脑/小脑/脑干/间脑等仿生神经节点是 domour 的核心认知模型，不能与具体的 Agent 类型（如 LLMAgent、SequentialAgent、ACP Agent）产生强耦合。

### 规范定型
- **领域抽象统一**: `internal/brain` 仅针对 `SensorySignal`（感觉信号）、`Thought`（宏观思考）、`TacticalAction`（战术动作）和 `MotorFeedback`（运动反馈）等仿生模型进行处理。
- **无感运行**: 无论外部使用何种 Agent 模式，`internal/brain` 均无需感知 Agent 类型，实现认知底座的最高复用性。

---

## 5. 基础设施单例与 redundancy 清理

### 经验教训
项目中曾同时存在 `internal/infra/bus` 与 `internal/infra/eventbus` 两个事件总线实现，导致事件总线职责模糊。

### 规范定型
- 统一收拢至 `internal/infra/eventbus/` 包下，包含内存本地总线 (`local`) 和 NATS 分布式总线 (`nats`)，彻底移除冗余包。
