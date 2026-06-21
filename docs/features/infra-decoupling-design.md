# 方案设计：基础设施组件依赖解耦与接口抽象方案

本项目致力于将 Domour 打造为一个能在云原生（Cloud）、边缘端（Edge）及 PC/嵌入式（Physical）环境弹性调度的智能体框架。
为达成此目标，需要遵循 **“接口与实现分离”** 的原则，将底层的缓存（Cache）、发布订阅（Pub/Sub）、数据库（DB）以及 Durable Agent 运行时与具体的基础设施依赖解耦。

---

## 1. 架构演进与权衡 (Architecture & Tradeoffs)

我们将解耦逻辑定调为 **“内核轻量 + 插件扩展”**：

1.  **Domour 核心库**：定义接口，并实现一套**“零外部依赖”**的本地版实现（基于内存或单机轻量文件）。
2.  **cosmos-star / 扩展项目**：实现分布式、集群化、云原生的生产级适配层（如基于 SurrealDB/NATS/Dapr 等重型组件）。

> [!NOTE]
> 这样做不仅能保证 Domour 可以随时被编译并集成在低算力的物理/嵌入式设备上（只需极简的本地实现），又能在部署至云端集群时无缝接入 Dapr 或云原生组件生态。

### 1.1 架构决策权衡 (ADR)

| 选型方案 | 适用场景 | 优势 | 劣势/放弃原因 |
| :--- | :--- | :--- | :--- |
| **方案 A: 强绑定具体依赖** | 单一架构场景 | 直接调用底层 SDK，无前期适配层开发开销。 | 内核极度臃肿，无法低成本地移植到受限的嵌入式设备或单机 CLI 场景。 |
| **方案 B: 仅提供抽象不提供实现** | 纯接口定义库 | 内核足够轻量干净。 | 开发者无法开箱即用，必须自己为每个基础设施写实现，对单机本地调试极其不友好。 |
| **方案 C: 内核接口+本地简单实现 (已采纳)** | **多端弹性部署** | **1. 轻量化**：本地版内存级实现不依赖外部数据库。<br>**2. 开发体验**：本地一键启动测试。<br>**3. 生产级扩展**：集群生产级逻辑完全移至外围生态项目。 | 需要定义和维护一套兼容多端语义的基础设施接口。 |

---

## 2. 接口设计与拆分规划

### 2.1 消息总线接口 (`bus.EventBus`)
*   **痛点**：当前代码强绑定了 NATS JetStream 的 API 和底层类型，业务层无法无缝替换其他总线。
*   **接口设计** (在 `internal/infra/eventbus/eventbus.go` 定义)：
    ```go
    package eventbus

    import "context"

    type Subscription interface {
        Unsubscribe() error
    }

    type EventBus interface {
        Publish(ctx context.Context, topic string, data []byte) error
        Subscribe(ctx context.Context, topic string, handler func(data []byte)) (Subscription, error)
        Close() error
    }
    ```
*   **具体实现**：
    *   `local` (Domour 内置)：基于 Go Channel & `sync.Map` 广播实现的本地进程内事件总线（适用于 PC / 离线环境）。
    *   `nats` (当前 NATS 代码封装重构)：重构使其隐藏 NATS 特有类型，仅向外输出 `EventBus` 接口。
    *   `dapr` (cosmos-star 提供)：基于 Dapr Pub/Sub 组件的集群级实现。

---

### 2.2 缓存接口 (`cache.Cache`)
*   **痛点**：当前 L2 缓存强依赖 `dgraph-io/badger` 数据库，难以适配云端多端统一管理的需求。
*   **接口设计** (在 `internal/infra/cache/cache.go` 定义)：
    ```go
    package cache

    import (
        "context"
        "time"
    )

    type Cache[V any] interface {
        Get(ctx context.Context, key string) (V, bool, error)
        Set(ctx context.Context, key string, value V, ttl time.Duration) error
        Delete(ctx context.Context, key string) error
        Close() error
    }
    ```
*   **具体实现**：
    *   `memory` (Domour 内置)：基于 Go 内存 Map（如 `sync.Map` 或带 TTL 的 LRU 缓存）。
    *   `badger` (重构现有的 L2 实现)：基于 Badger 作为单机磁盘 KV 的轻量级本地持久缓存。
    *   `surrealdb` (cosmos-star 提供)：基于 SurrealDB 的生产级高可用缓存。

---

### 2.3 数据库存储接口 (`storage.DB`)
*   **痛点**：当前的 `SurrealDB` 强依赖官方 Golang SDK。在无 SurrealDB 运行时的单机嵌入式设备中，Domour 业务层无法通过其他数据库进行妥善的本地存储。
*   **接口设计** (在 `internal/infra/storage/db.go` 定义)：
    ```go
    package storage

    import "context"

    type DB interface {
        Query(ctx context.Context, query string, vars map[string]any) (any, error)
        Create(ctx context.Context, table string, data any) (any, error)
        Update(ctx context.Context, id string, data any) (any, error)
        Delete(ctx context.Context, id string) (any, error)
        Close() error
    }
    ```
*   **具体实现**：
    *   `sqlite` (Domour 内置)：通过轻量级 SQLite 支持边缘/离线单机运行。
    *   `surrealdb`：支持图与文档多模态查询的 SurrealDB 实现。

---

### 2.4 Durable Agent 运行时引擎 (`dapr.DurableAgent`)
*   **痛点**：`DurableAgent` 引擎目前是强耦合 Dapr 侧车（Workflow & Actor 调度）的。在无 Dapr 运行时的情况下，Agent 无法在单机直接唤醒。
*   **流式输出（Stream）支持设计**：
    由于 Durable Workflow 框架具有确定性重放（Replay）特性，编排器（Orchestrator）无法直接持有活动的 gRPC/HTTP 连接进行 `stream.Send()`。
    我们采用**事件总线旁路流式推送（EventBus Bypass Streaming）**方案：
    1. 宿主服务（`AssistantService.Chat`）在启动工作流前，订阅基于该工作流 ID 的事件主题：`agent/workflow/{workflow_id}/stream`。
    2. 工作流中的 Activity 执行大模型对话或工具调用时，将产生的实时 Token 块发布到该事件主题。
    3. `AssistantService` 监听到事件后，通过原有接口的 `yield` 回调实时推送给客户端。
    4. 工作流重放时，由于已经执行成功的 Activity 会被跳过，因此不会产生重复的流事件，从而完美避免了流式输出重复的问题。
*   **接口设计** (在 `internal/infra/dapr/daprclient.go` 定义)：
    ```go
    package dapr

    import "context"

    type DurableAgentOrchestrator interface {
        StartWorkflow(ctx context.Context, workflowID string, input any) (string, error)
        GetWorkflowStatus(ctx context.Context, workflowID string) (any, error)
    }
    ```
*   **具体实现**：
    *   `local` (Domour 内置)：利用本地 Go Ticker / Channels 与本地缓存（如 Badger 或本地 SurrealDB 实例）结合，实现轻量级、具备“步骤记录与重试”机制的单机小脑循环（CerebellumNode）。
    *   `dapr` (当前的 DurableAgent 封装)：基于 Dapr Workflow 引擎，提供分布式环境的断点强力恢复保障。

---

## 3. 具体实现计划与实施步骤

为了确保在重构期间**功能不中断、代码时刻保持可编译、业务完全可测试**，重构必须严格按照以下增量顺序推进：

### 阶段一：Domour 内核抽象接口定义 (Interface Definition Phase)
*   **目标**：首先在 Domour 核心包中建立基础设施的接口声明，不改动现有实现。
*   **具体步骤**：
    1.  **定义接口**：在 Domour 核心包中新建对应的接口声明文件：
        *   `internal/infra/eventbus/eventbus.go` -> 定义 `EventBus` 和 `Subscription`。
        *   `internal/infra/cache/cache.go` -> 定义 `Cache[V any]`。
        *   `internal/infra/storage/db.go` -> 定义 `DB` 接口。
        *   `internal/infra/dapr/daprclient.go` -> 定义 `DurableAgentOrchestrator`。
    2.  **平滑过渡（暂不替换现有实现）**：
        *   现有的 `nats.go`、`l2.go` (badger) 和 `surreal.go` 保持原样，无需进行代码剥离，以便整个项目保持可编译、测试依然能跑通的状态。

### 阶段二：宇宙之星 (cosmos-star) 适配与生产级集成封装 (Cosmos-Star Adaptation Phase)
*   **目标**：将 cosmos-star 中已有的集群级基础设施组件封装为 Domour 新接口的实现（因为这些生产级组件可能还要供给 cosmos-star 自身的其他业务使用）。
*   **具体步骤**：
    1.  **实现适配器**：
        *   在 `cosmos-star` 中引入 Domour 刚刚定义的接口。
        *   编写适配器实现类（如 `cosmos.SurrealDBCache` 实现了 Domour 的 `cache.Cache` 接口，`cosmos.NatsEventBus` 实现了 Domour 的 `bus.EventBus` 接口）。这些实现内部复用了 `cosmos-star` 原有的连接池和基础设施集成。
    2.  **重构 Host 绑定层（Dependency Injection / DI 阶段）**：
        *   修改宿主模块 `domour-host` 的依赖注入和初始化逻辑（Bootstrap/Wire）。
        *   在运行时绑定（DI Binding）阶段，**显式地用 cosmos-star 的集群实现替换/注入到 Domour 接口的依赖项中**。
    3.  **端到端冒烟测试**：
        *   启动 `cosmos-star` 运行实例，利用其注入的分布式基础设施，执行端到端的 Agent 工作流和对话请求，**验证核心功能完全无丢失，并且可以通过分布式集成测试**。

### 阶段三：替换 Domour 本身代码为本地极简实现 (Domour Local Fallback Phase)
*   **目标**：当云端/生产级别链路完全稳定并得到验证后，开始对 Domour 内部做“减法”，提供本地极简的单机/离线运行体验。
*   **具体步骤**：
    1.  **实现本地极简版组件**：
        *   在 Domour 中编写 `infra/bus/local`：基于原生 Go Channel 实现的简易进程内事件分发总线。
        *   在 Domour 中编写 `infra/cache/memory`：基于 `sync.Map` 锁实现的基础缓存。
        *   在 Domour 中编写 `infra/storage/sqlite`：基于 SQLite 的单文件本地数据库（可复用现有的 SQL 查询结构作为轻量图/文档数据库替代）。
        *   在 Domour 中编写 `infra/dapr/local`：不依赖 Dapr 侧车，使用 Go Ticker + SQLite 实现的基础步骤调度回溯小脑循环。
    2.  **替换 Domour 内置强依赖**：
        *   将 `session.Manager` 等内部组件强引用的 `storage.SurrealDB` 等修改为引用 `storage.DB` 接口。
        *   删除 Domour 核心包里不需要的基础设施第三方 SDK 依赖，减轻内核体积。
    3.  **最终测试验证**：
        *   断开外部网络和分布式组件，以 Local 模式独立启动 Domour，验证单机 CLI / 嵌入式生存模式下的基本闭环，跑通所有本地单元测试。


---

## 4. 验证方案 (Verification Plan)

### 4.1 单元测试与接口集成测试
*   为 `infra/cache`, `infra/bus`, `infra/storage` 编写通用测试套件（TCK）。
*   无论使用 `memory/local/sqlite` 本地版本，还是重构后的 `badger/nats/surrealdb` 版本，都必须跑通同样的测试集，确保它们接口语义 100% 对等。

### 4.2 业务级验证
*   在单节点无 Dapr / 无外部集群依赖的环境下，使用配置参数启动 Domour CLI，验证本地会话管理、L1/L2 缓存与智能体推理流程是否能流畅闭环。
