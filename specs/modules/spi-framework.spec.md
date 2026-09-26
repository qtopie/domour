# Module Spec: SPI Framework & Decoupling

## 1. Overview
本模块定义 Domour 的标准服务提供者接口（Service Provider Interface, SPI）三层规范体系，并彻底消除业务层（`internal/app/assistant`）对特定中间件（`internal/infra/dapr`）专有类型的硬编码引用，实现业务调度层与基础设施运行时的正交解耦。

## 2. Interface / API Contract

### 2.1 三层 SPI 目录与接口规范 (`ark/spi/`)
- **Runtime SPI (`ark/spi/runtime/`)**:
  - `Router`: 负责请求分类、意图识别与推理模式分流（Simple/ReAct/DeepThink/Planner）。
  - `Cognitor`: 认知推理模型抽象（主核 Main 与协助核 Secondary 统一接口，通过 Tags 区分）。
  - `PreTurnHook` / `PostTurnHook`: 轮次生命周期微控制器。
- **Capability SPI (`ark/spi/capability/`)**:
  - `ToolInvoker`: 统一 Native/CLI/MCP 工具调用的执行边界。
  - `SkillRegistry`: 任务级技能注册与发现契约。
- **Infra SPI (`ark/spi/infra/`)**:
  - `AgentOrchestrator`: 智能体工作流/ReAct 调度器接口。
  - `WorkflowInput` / `WorkflowState`: 中性工作流输入参数与执行状态结构体。
  - `SessionStore` / `StateStore`: 会话历史持久化与通用 KV 状态接口。
  - `EventBus`: 进程内/分布式事件分发接口。
  - `Cache`: 泛型缓存抽象。

### 2.2 核心解耦契约
- `WorkflowInput`: 采用 Go 原生类型与 `json.RawMessage` 封装消息与模型参数，严禁 import `github.com/dapr/go-sdk`。
- `WorkflowState`: 封装执行状态、结果与错误，与 Dapr 工作流解耦。
- `internal/app/assistant/chat.go` 与 `copilot.go` 仅依赖 `internal/engine` 或 `ark/spi/infra` 中的中性工作流结构体。

## 3. Acceptance Criteria (BDD)

### Feature: 消除业务层对 Dapr 的类型泄漏

#### Scenario 1: [SPEC-SPI-001] 中性工作流输入输出与序列化完备性
- **Given** 创建一个包含会话 ID、模型参数、阶段标签与消息体的 `WorkflowInput`
- **When** 将其序列化为 JSON 并传递给工作流编排器
- **Then** 工作流编排器能够完整还原消息与参数，并返回符合 `WorkflowState` 契约的状态结果
- **Mapped Test:** `ark/spi/infra/orchestrator_test.go:TestWorkflowInput_Serialization`

#### Scenario 2: [SPEC-SPI-002] 业务层零 Dapr 包导入约束
- **Given** 检查 `internal/app/assistant` 下所有 Go 源码文件
- **When** 扫描代码中的 `import` 语句
- **Then** 不得出现 `github.com/dapr/` 或 `internal/infra/dapr` 依赖
- **Mapped Test:** `ark/spi/infra/orchestrator_test.go:TestNoDaprDependencyInAppAssistant`

#### Scenario 3: [SPEC-SPI-003] LocalOrchestrator 契约对齐与执行闭环
- **Given** 构造 `LocalOrchestrator` 实例作为默认引擎
- **When** 使用中性 `WorkflowInput` 调用 `StartWorkflow` 与 `GetWorkflowStatus`
- **Then** 工作流能在进程内正常执行完成，状态为 completed 或 failed，且不触发任何外部 Dapr RPC
- **Mapped Test:** `ark/spi/infra/orchestrator_test.go:TestLocalOrchestrator_NeutralInput`

### Feature: 神经双通路路由与主协核心协同 (Dual-Pathway Routing & Fast Calibration)

#### Scenario 4: [SPEC-SPI-004] 低通快反直派：轻量/问候/即时计算任务直接分流至 Secondary Core
- **Given** 输入轻量问候、参数计算或即时校准请求（如 "ping", "hello", "calc"）
- **When** 调用 `Router.Route` 进行任务意图分析
- **Then** 决策目标核心为 `CoreSecondary`，且标记 `FastPath=true`，直接由 Secondary Core 处理而绕过昂贵的大模型规划
- **Mapped Test:** `ark/spi/runtime/router_test.go:TestDefaultDualPathwayRouter_Routing`

#### Scenario 5: [SPEC-SPI-005] 高通深度规划：复杂长链任务分流至 Primary Core
- **Given** 输入需要多步推理、架构设计或代码重构的复杂意图
- **When** 调用 `Router.Route` 进行任务意图分析
- **Then** 决策目标核心为 `CorePrimary`，且标记 `FastPath=false`，交由 Primary Core 实施宏观任务分解
- **Mapped Test:** `ark/spi/runtime/router_test.go:TestDefaultDualPathwayRouter_Routing`

### Feature: DI 依赖注入与零依赖单机运行 (DI Containerization & Zero-Dependency Standalone)

#### Scenario 6: [SPEC-SPI-006] App 函数选项依赖注入完备性
- **Given** 外部宿主提供自定义的 `SessionStore`、`AgentOrchestrator`、`EventBus` 与 `Router`
- **When** 使用 `WithStore`、`WithOrchestrator`、`WithEventBus`、`WithRouter` 构造 `App`
- **Then** `App` 成功注入外部组件，优先使用注入实例并跳过内部默认硬编码初始化
- **Mapped Test:** `internal/app/assistant/app_test.go:TestNewApp_FunctionalOptions`

#### Scenario 7: [SPEC-SPI-007] 零外部依赖单机启动与内存存储默认就绪
- **Given** 未提供任何外部 Dapr、SurrealDB 或 NATS 环境配置
- **When** 调用 `NewApp(nil)` 默认启动应用
- **Then** 系统无报错启动，存储层自动回退至纯内存 `MemoryStore`，事件总线自动装载 `LocalEventBus`，具备开箱即用能力
- **Mapped Test:** `internal/app/assistant/app_test.go:TestNewApp_ZeroDependencyDefault`


