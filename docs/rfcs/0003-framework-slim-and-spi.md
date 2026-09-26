# RFC-0003: Domour 框架瘦身与三层 SPI 解耦方案

- **状态:** 已评审并达成共识（ACCEPTED / READY FOR SPEC）
- **日期:** 2026-09-26
- **关联 TASK:** `.agents/TASK.md` → 插件化扩展机制与 SPI 瘦身改造

---

## 1. 背景与核心目标

当前 domour 核心库存在最主要的架构问题是**重型基础设施强依赖与类型穿透**：
- 核心代码直接引入并绑定了 Dapr、SurrealDB、BadgerDB、NATS 等重型外部 SDK。
- 业务层（如 `chat.go`、`copilot.go`）直接使用了 `dapr.AgentWorkflowInput` 等专有结构体，导致单机无 Dapr/Surreal 环境时启动困难，无法作为纯粹轻量的 Agent SDK 使用。

为了控制重构爆炸半径（Blast Radius）、确保系统稳定性与向后兼容：
- **内部核心保持克制**：保留现有 `internal/brain/` 及内部生物隐喻实现与神经通路逻辑，不做大规模破坏性删除或重命名。
- **边界建立标准 SPI**：在公共 SDK 层 `ark/spi/` 抽象清晰、现代的三层标准 SPI（Runtime / Capability / Infra），屏蔽底层实现细节。
- **零外部依赖默认可用**：默认启动走纯内存（Memory）与本地文件（File）轻量实现，外部生产级基础设施（Dapr 工作流、SurrealDB 会话存储等）由宿主工程（如 `cosmos-star`）通过 DI 显式注入。

---

## 2. 核心设计原则

1. **零外部基础设施依赖（Zero-Dependency Core）**：
   - 默认模式下，Domour 仅凭 Go 标准库和轻量本地组件即可启动运行（内存或本地 JSON 文件）。
   - 彻底解除业务代码对 Dapr、SurrealDB、NATS、Badger 的硬编码显式依赖。
2. **内稳外开（Internal Stability, External SPI）**：
   - 内部架构（包括 `internal/brain/`、`internal/bionic/`、`internal/engine/`）保持现有机制稳定，不增加额外重构风险。
   - 对外暴露的三层 SPI 接口采用标准现代计算机与 Agent 规范命名（Router, Cognitor, Lifecycle Hook, ToolInvoker, Store, Orchestrator），便于外部宿主和第三方开发者对齐生态。
3. **“一切皆 Native Module”驱动解耦**：
   - 所有外部可扩展能力统一通过 `ark/spi/` 规范化。
   - 外部宿主工程（如 `cosmos-star`、桌面客户端）可自由提供云原生或分布式生产级实现，通过依赖注入（DI）接入。

---

## 3. 完整三层 SPI 规范设计 (`ark/spi/`)

外部用户与宿主可通过实现以下三层 SPI 接口来全方位扩展 Domour。

```
ark/spi/
├── runtime/          # Layer 1: Agent 核心调度与认知运行时
│   ├── router.go     # ① Router：请求分类、意图分流与推理模式决策
│   ├── cognitor.go   # ② Main Cognitor & ③ Secondary Core：主干大模型与辅助模型
│   └── lifecycle.go  # ④ Lifecycle Hooks：前置/后置微控制器（守卫、审计、记忆写回）
├── capability/       # Layer 2: 智能体能力扩展
│   ├── tool.go       # ⑥⑦⑧ ToolInvoker：MCP、CLI、Native 统一工具接口
│   └── skill.go      # ⑤ SkillRegistry：技能发现、编排契约与组合路由
└── infra/            # Layer 3: 底层基础设施与持久化
    ├── store.go      # ⑨ SessionStore & StateStore：会话历史与通用 KV 快照
    ├── eventbus.go   # ⑩ EventBus：进程内/分布式事件发布订阅
    ├── orchestrator.go # ⑪ AgentOrchestrator：ReAct / 工作流调度引擎
    └── cache.go      # ⑫ Cache：L1 内存 / L2 缓存抽象
```

---

### Layer 1: Agent 运行时 SPI (`ark/spi/runtime/`)

#### 3.1 路由与决策器 (`router.go`)
用于接管请求进入时的第一站，基于神经双通路模型（Low Road vs High Road）决定是直派 Secondary Core（小脑快反/校准）还是递交 Primary Core（大脑宏观规划）。

```go
package runtime

import "context"

type CoreTarget string

const (
    CorePrimary   CoreTarget = "primary"   // 主核心（大脑）：复杂推理、长链规划、深度思考
    CoreSecondary CoreTarget = "secondary" // 协助核心（小脑）：轻量快反、即时校准纠错、短反射
)

// RouteDecision 描述 Router 做出的双通路调度决策。
type RouteDecision struct {
    TargetCore   CoreTarget        `json:"target_core"`             // "primary" | "secondary"
    FastPath     bool              `json:"fast_path"`               // 是否命中低通快反通道
    Mode         string            `json:"mode"`                    // 推理模式: "simple" | "react" | "deep_think" | "planner"
    RequiredTags []string          `json:"required_tags,omitempty"` // 必要标签 (如 "deep", "reasoning")
    PreferTags   []string          `json:"prefer_tags,omitempty"`   // 偏好标签 (如 "flash", "local")
    Meta         map[string]string `json:"meta,omitempty"`          // 路由透传元数据
}

// Router 负责解析请求并决策调度路径与推理模式。
type Router interface {
    Route(ctx context.Context, sessionID, message string, meta map[string]string) (RouteDecision, error)
}
```

#### 3.2 认知推理引擎 (`cognitor.go`)
- **Primary Core（主核心）**：负责处理复杂高阶任务，涵盖多步规划、深度推理（Deep Think）、复杂决策逻辑与重度工具链编排。
- **Secondary Core（协助核心）**：负责处理轻量任务与快速校准，涵盖即时反射响应、意图快速校准、工具调用结果即时校验纠错、安全护栏拦截、快速感知抽取以及异步辅助（如记忆提炼等）。
两者同构共用统一的 `Cognitor` 抽象接口，由调度层根据模型能力标签（如 `pro`/`deep` vs `flash`/`lite`/`local`）与场景需求动态调度。

```go
package runtime

import "context"

type Message struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type CognitorRequest struct {
    Messages    []Message `json:"messages"`
    Temperature float32   `json:"temperature,omitempty"`
    MaxTokens   int       `json:"max_tokens,omitempty"`
}

type CognitorResponse struct {
    Content      string `json:"content"`
    Provider     string `json:"provider"`
    Model        string `json:"model"`
    TokenUsage   int    `json:"token_usage,omitempty"`
}

type CognitorChunk struct {
    Type    string `json:"type"` // "text" | "thought" | "error"
    Content string `json:"content"`
    Done    bool   `json:"done"`
    Err     error  `json:"-"`
}

// Cognitor 是 LLM 推理后端的统一抽象。
// Main Cognitor 与 Secondary Core 均为此接口的实例，依靠注册中心的 Tags 区分。
type Cognitor interface {
    Generate(ctx context.Context, req *CognitorRequest) (*CognitorResponse, error)
    Stream(ctx context.Context, req *CognitorRequest) (<-chan CognitorChunk, error)
    IsReady(ctx context.Context) (bool, error)
}
```

#### 3.3 轮次生命周期钩子 (`lifecycle.go`)
提供前置/后置轻量透明的生命周期微控制器，用于权限守卫、速率限制、安全 Veto、记忆整合、遥测审计等。

```go
package runtime

import "context"

type TurnContext struct {
    SessionID string
    UserQuery string
    Meta      map[string]string
}

// PreTurnHook 在进入核心推理循环前执行，若返回 error 则终止并拒绝当前轮次。
type PreTurnHook interface {
    Name() string
    Priority() int
    BeforeTurn(ctx context.Context, tc *TurnContext) error
}

// PostTurnHook 在一轮推理和执行完成后执行（如异步记忆整合、安全审计）。
type PostTurnHook interface {
    Name() string
    Priority() int
    AfterTurn(ctx context.Context, tc *TurnContext, finalReply string) error
}
```

---

### Layer 2: 智能体能力 SPI (`ark/spi/capability/`)

#### 3.4 统一工具调用器 (`tool.go`)
所有工具（不管是进程内 Go 函数、本地 CLI 子进程，还是外部 MCP 协议服务）对外表现完全同构。

```go
package capability

import (
    "context"
    "encoding/json"
    "io"
)

type ToolSpec struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"input_schema"` // 标准 JSON Schema
}

// ToolInvoker 统合 In-Process Native、CLI 进程与 MCP 服务的工具执行边界。
type ToolInvoker interface {
    Spec() ToolSpec
    Invoke(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
    io.Closer
}
```

#### 3.5 技能注册表 (`skill.go`)
承载任务级技能编排契约与复合注册中心。

```go
package capability

import "context"

type SkillDefinition struct {
    ID           string   `json:"id"`
    Name         string   `json:"name"`
    Description  string   `json:"description"`
    IntentTags   []string `json:"intent_tags"`
    Instructions string   `json:"instructions"`
    Tools        []string `json:"tools"`
}

type SkillRegistry interface {
    Register(ctx context.Context, s *SkillDefinition) error
    Get(ctx context.Context, id string) (*SkillDefinition, error)
    List(ctx context.Context) ([]*SkillDefinition, error)
    Delete(ctx context.Context, id string) error
}
```

---

### Layer 3: 基础设施与持久化 SPI (`ark/spi/infra/`)

#### 3.6 调度编排器 (`orchestrator.go`)
彻底清除对 `github.com/dapr/go-sdk` 的类型穿透，采用中性结构体定义输入与状态。

```go
package infra

import (
    "context"
    "encoding/json"
)

type WorkflowInput struct {
    SessionID   string          `json:"session_id"`
    Messages    json.RawMessage `json:"messages"`
    Provider    string          `json:"provider"`
    Model       string          `json:"model"`
    Stage       string          `json:"stage"`
    StreamFinal bool            `json:"stream_final"`
    Meta        map[string]string `json:"meta,omitempty"`
}

type WorkflowState struct {
    WorkflowID string          `json:"workflow_id"`
    Status     string          `json:"status"` // "running" | "completed" | "failed"
    Result     json.RawMessage `json:"result,omitempty"`
    Error      string          `json:"error,omitempty"`
}

// AgentOrchestrator 调度工作流循环（内置默认提供 LocalOrchestrator，外部宿主可注入 Dapr 实现）。
type AgentOrchestrator interface {
    StartWorkflow(ctx context.Context, workflowID string, input WorkflowInput) (string, error)
    GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowState, error)
    WaitForWorkflow(ctx context.Context, workflowID string) (*WorkflowState, error)
}
```

#### 3.7 存储 (`store.go`)、事件总线 (`eventbus.go`) 与缓存 (`cache.go`)

```go
package infra

import (
    "context"
    "time"
)

// SessionStore 管理会话与消息历史。
type SessionStore interface {
    GetSession(ctx context.Context, sessionID string) (any, error)
    SaveSession(ctx context.Context, sess any) error
    AppendHistory(ctx context.Context, sessionID string, msg any) error
    Close() error
}

// StateStore 通用 KV 状态存取（无依赖默认使用内存或单机 JSON 文件）。
type StateStore interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, val []byte) error
    Delete(ctx context.Context, key string) error
    Close() error
}

// EventBus 事件发布订阅。
type EventBus interface {
    Publish(ctx context.Context, topic string, data []byte) error
    Subscribe(ctx context.Context, topic string, handler func(data []byte)) (Subscription, error)
    Close() error
}

type Subscription interface {
    Unsubscribe() error
}

// Cache 泛型缓存抽象。
type Cache[V any] interface {
    Get(ctx context.Context, key string) (V, bool, error)
    Set(ctx context.Context, key string, value V, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Close() error
}
```

---

## 4. 默认内置实现与外部适配器分布

| SPI 接口 | 核心内置默认实现（零外部依赖） | 宿主外部实现（由 cosmos-star 提供） |
| :--- | :--- | :--- |
| `runtime.Router` | `SimpleRouter`（静态/配置匹配） | `DynamicSemanticRouter` |
| `runtime.Cognitor` | `InTreeCognitor`（直连主流 API / llama.cpp） | `GatewayCognitor` / 集中集群调度 |
| `runtime.PreTurnHook` | `SecurityVetoHook` / `NoopHook` | `EnterpriseRBACGuard` |
| `runtime.PostTurnHook` | `BackgroundPostHook`（可异步驱动 Secondary Core 执行校准/记忆写回） | `DistributedVectorIndexWriter` |
| `capability.ToolInvoker` | `NativeTool` / `CLITool` / `InTreeMCP` | `SandboxedWasmTool` / `RemoteGRPCTool` |
| `capability.SkillRegistry`| `CompositeRegistry`（内存 + 本地 Markdown 文件）| `DaprStateSkillRegistry` |
| `infra.AgentOrchestrator`| `LocalOrchestrator`（进程内直接执行 ReAct 循环） | `DaprDurableWorkflowOrchestrator` |
| `infra.SessionStore` | `MemoryStore` / `FileStore`（单机原子写入） | `SurrealSessionStore` / `BadgerStore` |
| `infra.EventBus` | `LocalEventBus`（Go Channels 广播） | `NatsEventBus` / `DaprPubSubBus` |
| `infra.Cache` | `MemoryCache`（并发带 TTL 内存缓存） | `RedisCache` / `BadgerL2Cache` |

---

## 5. 依赖剥离与去重策略

### `go.mod` 瘦身目标
在迁移完成后，从核心 `go.mod` 彻底剔除以下重型包：
- `github.com/dapr/dapr`
- `github.com/dapr/go-sdk`
- `github.com/surrealdb/surrealdb.go`
- `github.com/dgraph-io/badger/v4`
- `github.com/nats-io/nats.go`

---

## 6. 实施路线图（分阶段演进）

### Phase 1: 契约建立与类型解耦 (P0 - 消除类型泄漏)
- [x] 创建 `ark/spi/runtime/`、`ark/spi/capability/`、`ark/spi/infra/` 目录结构与接口定义。
- [x] 在 `ark/spi/infra/orchestrator.go` 引入中性 `WorkflowInput` / `WorkflowState`。
- [x] 修改 `internal/engine/orchestrator.go`，剔除对 `dapr.AgentWorkflowInput` 的 type alias 别名。
- [x] 改造 `internal/app/assistant/chat.go`、`copilot.go` 与 `autopilot.go`，消除 `import "internal/infra/dapr"`。
- [x] 运行 `./scripts/check.sh` 与全量测试。

### Phase 2: DI 容器化与默认无依赖运行 (P0 - 零依赖就绪)
- [ ] 改造 `App` 结构体，支持 `WithStore`、`WithOrchestrator`、`WithEventBus`、`WithRouter` 函数选项。
- [ ] 移除 `app.go` 中的硬编码 `InitStore()`，默认使用 `MemoryStore` / `FileStore` 与 `LocalOrchestrator`。
- [ ] 完善 Secondary Core 调度机制（支持轻量任务、即时校准纠错与辅助提炼）。
- [ ] 验证在没有任何外部数据库/中间件的前提下单机正常启动并响应请求。

### Phase 3: 外部适配器交接与依赖彻底剥离 (P1 - 瘦身收尾)
- [ ] 将 Dapr、SurrealDB、Badger、NATS 等适配器移交至 `cosmos-star`（或隔离为独立可选扩展）。
- [ ] 执行 `go mod tidy` 彻底净化 `go.mod`。
- [ ] 完成全仓库验收与端到端测试。

### Phase 4: cosmos-star 端到端集成验证 (P2 - 生态对接)
- [ ] 宿主侧完成生产级 SPI 注入。
- [ ] 运行端到端集群与分布式验证。
