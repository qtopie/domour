# Brain 设计

## 1. 目标

Brain 是 Domour 的**思考层**，负责理解、规划、补全和建议生成，但**不直接拥有最终输出权、工具执行权或渲染权**。  
在目标架构中，Brain 会从当前的本地实现演进为一个可水平扩展的集群服务，并通过 Dapr 参与服务发现、状态协作和事件治理。

一句话定义：

- **Brain 负责“怎么想”**
- **Motor 负责“能不能做、怎么做、怎么对外返回”**

---

## 2. 职责边界

### Brain 负责

- 对用户输入做语义理解与意图补全
- 生成对话回复草稿、代码建议、任务计划、图结构草案
- 将复杂目标拆成可执行的中间意图或步骤
- 在需要时持续流式输出 chunk，供 Motor 旁路监听和裁决
- 当上游模型不可用时，返回规则化 fallback 结果，避免链路中断

### Brain 不负责

- 不直接面向客户端返回最终结果
- 不直接调用高权限工具
- 不直接做最终渲染
- 不直接决定是否执行危险动作
- 不持有 session 的最终 owner 身份

这条边界是整个系统的核心安全约束：**Brain 可以想错，但 Motor 不能做错。**

---

## 3. 当前实现

当前 Brain 仍以内嵌本地 client 方式存在，主要能力通过 `internal/pkg/agent/local_brain.go` 暴露：

- `StreamChat`
- `StreamCopilot`
- `StreamAutopilot`
- `ChatReply`
- `PlanDiagram`
- `Copilot`
- `Autopilot`

当前行为特点：

- Chat active 路径中，Brain 以 goroutine 方式向 `SessionBridge.BrainOut` 写入事件
- Chat 图片路径中，Motor 会并行做一层轻量 OCR / 关键事实提取；如果在初始拦截窗口内拿到结果，就把这层上下文补丁注入到 Brain 的首轮 prompt 里，尽量降低图片理解场景的早期事实错误
- Copilot active 路径中，Brain 也可流式输出 `copilot_chunk`
- Autopilot / Copilot normal 路径中，Brain 作为 Motor 的旁路思考器，仅在 Motor 判定任务复杂时参与
- 图类请求由 Brain 产出 D2/结构化草案，再交给 Motor 统一渲染
- CLI provider 失败时，Brain 会退回本地规则化 fallback
- Brain 请求已预留多模态附件通道；当前先支持文本 + 图片输入，音频/视频作为后续扩展
- 当前这层图片 OCR 拦截仍属于“上下文校正”，不是底层共享 KV cache；Motor 与 Brain 共享的是最小事实上下文，而不是同一个模型原生上下文窗口

当前实现已经具备未来集群化最关键的抽象前提：**Agent 依赖的是 `BrainClient` 接口，而不是具体本地实现。**

---

## 4. 目标架构

### 4.1 服务角色

集群版建议拆成以下角色：

- `agent-gateway`：对外入口，维持客户端连接
- `motor-service`：session owner、最终输出与安全网关
- `brain-service`：思考与规划集群
- `relay-service`：承接实时流式桥接
- `state-store`：保存 session owner、lease、bridge 元数据

其中 Brain 的定位是：

- 可水平扩展
- 尽量无状态
- 专注生成中间结果
- 不与真实工具权限耦合

### 4.2 集群关系图

```d2
direction: right
theme: 200

Client: Client {
  shape: person
}

AgentGateway: agent-gateway {
  style: {
    fill: "#e3f2fd"
    stroke: "#1e88e5"
  }
  API: "gRPC / HTTP"
}

MotorPod: motor-service {
  style: {
    fill: "#fff3e0"
    stroke: "#fb8c00"
  }
  MotorCore: "motor core\n(final owner)"
  MotorDapr: "dapr sidecar"
}

BrainPod: brain-service {
  style: {
    fill: "#f3e5f5"
    stroke: "#8e24aa"
  }
  BrainCore: "brain core\n(plan / suggest / stream)"
  BrainDapr: "dapr sidecar"
}

Relay: relay-service {
  style: {
    fill: "#e8f5e9"
    stroke: "#43a047"
  }
  Bridge: "stream relay\n(SessionBridge remote)"
}

StateStore: state-store {
  style: {
    fill: "#ede7f6"
    stroke: "#5e35b1"
  }
  SessionMeta: "session owner / lease / bridge"
}

PubSub: pubsub {
  style: {
    fill: "#fce4ec"
    stroke: "#d81b60"
  }
  Audit: "audit / async events"
}

Client -> AgentGateway.API: request
AgentGateway.API -> MotorCore: session traffic

MotorCore -> BrainDapr: invoke brain via Dapr
BrainDapr -> BrainCore: service discovery

BrainCore -> Bridge: chunk / plan / diagram
MotorCore -> Bridge: read stream / send control

MotorDapr -> SessionMeta: owner / lease
BrainDapr -> SessionMeta: request metadata

MotorDapr -> Audit: refusal / completion
BrainDapr -> Audit: brain lifecycle

MotorCore -> Client: final output only

note: {
  label: "Rule:\nBrain produces intermediate results.\nMotor owns final output, tool execution, and safety."
}

note -> MotorCore
note -> BrainCore
```

### 4.3 推荐链路

#### Chat active

`agent -> motor -> brain(stream) -> motor -> client`

- 客户端只连接 agent / motor
- motor 启动 brain 请求
- brain 连续输出 chunk
- motor 旁路监听 brain 输出，并决定：
  - 原样转发
  - 补充工具结果
  - 渲染
  - 拒绝
  - 中断

#### Copilot normal / Autopilot normal

`agent -> motor -> brain -> motor`

- motor 先做一轮本地筛选
- 简单任务直接由 motor 返回
- 复杂任务再请求 brain 给出计划/建议
- 最终仍只由 motor 对外输出

---

## 5. Brain 的流式协议定位

Brain 不应该直接暴露“给用户看”的流，而应该暴露“给 Motor 消费”的流。  
因此 Brain 输出的不是最终 UI 结果，而是**中间语义事件**。

建议统一为以下事件模型：

| 事件 | 含义 |
| --- | --- |
| `chunk` | 普通文本/建议片段 |
| `done` | Brain 输出完成 |
| `refine` | 建议 Motor 做补充或修正 |
| `plan` | 结构化任务计划 |
| `diagram` | 图草案或结构描述 |
| `error` | Brain 侧错误 |

Motor 到 Brain 的控制事件建议保留：

| 控制 | 含义 |
| --- | --- |
| `stop` | 终止当前流 |
| `refuse` | Brain 输出被安全策略拒绝 |
| `retry` | 请求更换表达或缩小范围 |

这样本地实现可以继续用 channel，集群实现则可以换成独立 relay，而不改上层语义。

---

## 6. Dapr 在 Brain 设计中的位置

推荐把 Dapr 作为**控制面**，而不是唯一的数据面。

### 适合交给 Dapr 的部分

- Brain 服务发现与服务调用
- 配置分发
- Secret 管理
- Session 元数据持久化
- 事件审计与异步通知
- Trace / Metrics / Resiliency

### 不建议完全依赖 Dapr 的部分

- token/chunk 级别的高频实时流
- `SessionBridge` 的逐片段双向控制
- provider runtime 的本地恢复语义

原因很简单：Brain 到 Motor 的交互是高频、低延迟、强时序的；如果完全依赖通用 service invocation 或 pubsub，会让流控、背压、取消和 owner 管理都变复杂。

所以更推荐：

- **Dapr 负责发现和治理**
- **专用 gRPC relay 负责 stream**

---

## 7. 集群版 Brain 的状态策略

Brain 本身应尽量保持无状态，但以下状态需要可追踪：

- `session_id`
- `bridge_id`
- `motor_owner`
- `brain_request_id`
- `mode`
- `provider`
- `provider_runtime_id`

推荐分三层：

### 短期状态

- 当前请求上下文
- 已输出 chunk 计数
- 当前 cancellation token

只保存在进程内。

### 中期状态

- session owner
- lease
- bridge 元信息
- 最近一次 brain 响应摘要

放入 Dapr state store。

### 长期状态

- 可复用的成功规划模板
- 历史任务样例
- 审计日志

可进入外部 DB / 向量库，但不应阻塞主链路。

---

## 8. Provider Runtime 与集群约束

当前 Brain 的 CLI provider runtime 是按 `session + provider` 绑定到本地目录的。  
这对单机可行，但在集群中会引出两个问题：

当前代码里已经把所有 LLM 调用收口到 `diencephalon`（间脑）层：  
`brain` 只负责语义组织与路由，provider 选择、模型构造、文本生成、tool binding 等外部细节统一由间脑执行。

1. 同一个 session 被调度到不同 Brain pod 时，resume/continue 语义会失效
2. provider 的本地认证态和缓存态无法天然跨 pod 共享

因此集群版建议分阶段处理：

### 阶段一：session sticky

- 同一 `session_id` 尽量路由到同一个 Brain 实例
- 先保证 CLI runtime 可继续工作

### 阶段二：runtime 外置或专门化

- 将 provider runtime 单独抽成 worker / actor
- 普通 brain-service 只负责语义编排，不直接持有 provider 本地状态

如果不这样分，Brain 看起来能扩容，但实际会被本地 runtime 粘住。

---

## 9. 安全模型

Brain 的安全设计不是“自己绝对安全”，而是“默认不拥有危险能力”。

推荐规则：

- Brain 只输出建议，不直接执行
- Brain 输出默认视为**不可信中间结果**
- Motor 必须对 Brain 输出做：
  - 策略审查
  - 结构补全
  - 工具执行授权
  - 渲染与最终包装

这也意味着：

- Brain 可以更激进地探索答案
- Motor 负责把探索结果收敛到安全可执行结果

---

## 10. 演进路线

### 第一步：接口稳定

先稳定 `BrainClient` 的远程语义，不急着上完整集群能力：

- 保持 chat/copilot/autopilot 三类 Brain 能力接口一致
- 将本地事件抽象成更通用的远程事件模型

### 第二步：Dapr unary 化

先让非流式路径走 Dapr：

- `Autopilot` normal
- `Copilot` normal

这一步最容易验证服务发现和跨实例调用是否可靠。

### 第三步：接入 relay

把 active chat / active copilot 的 Brain 流式输出从本地 `SessionBridge` 迁移到远程 relay：

- Brain 写 relay
- Motor 读 relay
- Motor 保持最终 owner

### 第四步：runtime 去本地化

把 provider runtime 从普通 Brain pod 中解耦出来，降低 sticky 依赖。

---

## 11. 结论

Brain 在集群版里应该是一个**可扩展的思考服务**，而不是一个“直接回答用户、直接执行工具”的全权 Agent。

最关键的设计原则只有三条：

1. **Brain 只负责生成中间结果**
2. **Motor 永远持有最终输出和执行权**
3. **Dapr 负责治理，stream 走专用通道**

按这个边界推进，当前本地 goroutine + `SessionBridge` 架构可以自然演进到集群版，而不用推翻重做。
