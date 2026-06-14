# 目标架构（Target State）

本文档描述目标系统分层，不代表这些能力已经全部实现。

## 1. 设计原则

1. **现状与目标分离**：任何目标能力都必须允许降级到当前实现。
2. **Skill 与 Plugin 分离**：Skill 负责决策与编排，Plugin 负责 UI 或专用执行面。
3. **高风险动作必须可审计**：自动化不等于绕过确认、授权和回滚。
4. **cosmos-star 可插拔**：系统应在没有 cosmos-star 的情况下仍能工作。
5. **存储与计算分离，协议直通 (Storage-Compute Separation, Protocol Passthrough)**：针对 SurrealDB 等具备高级特性（图查询、实时监听）的数据库，利用 Dapr 或 P2P 机制进行节点发现与连接路由，但业务层必须保持原生协议（如 SurrealQL over WebSocket）直通，严禁因 Dapr State Store 的 KV 抽象而牺牲数据库的核心能力。
6. **接口设计与实现分离 (Separation of Interface & Implementation)**：在小脑（编排与执行）、脑干（底座与通信）、间脑（LLM 代理接口）等各个层次上，必须充分遵循接口与具体实现解耦的原则。智能体应既能在分布式 Dapr 托管环境中运行（通过 DurableAgent 保证长周期状态恢复），也能在无 Dapr 的单机/嵌入式边缘场景下平滑降级，以保证协议的兼容性和部署的灵活性。


## 2. 目标分层

### 2.1 终端层：`cosmos-assistant`

职责：

- 桌面 UI 与交互承载
- 本地状态、设置、通知、窗口与系统集成
- 插件资源承载与安全边界执行
- 人机协作确认、审阅、授权与审计展示

### 2.2 编排层：Domour

职责：

- 意图理解
- 任务分解
- 技能路由
- 执行计划生成
- 结果汇总与协同输出

**架构原则：Eino Inside, Dapr Outside**
- **Eino (微观思维)**: 作为 Domour 的核心逻辑引擎，负责具体的推理图 (Graph)、反思循环和工具路由。它代表了 Agent 的“智商”。
- **Dapr Agents (宏观外壳)**: 作为可选的托管与协同框架，负责分布式状态持久化 (Durable Workflow) 和跨节点事件总线。它代表了 Agent 的“体质”。

建议把 Domour 看成 **agent orchestrator**，而不是所有能力都内聚在一个模型里。

### 2.3 执行层：Skills + Tools + Plugins

这一层拆成 3 个概念：

- **Skill**：任务级能力定义，描述什么时候用、怎么用、可以调用什么
- **Tool**：具体动作接口，如 MCP、HTTP、数据库、gRPC、命令执行
- **Plugin**：面向用户的 UI 面板，或面向宿主的专用执行扩展

边界如下：

- Skill 负责“决策和编排”
- Tool 负责“执行原子动作”
- Plugin 负责“呈现或封装特定领域能力”

### 2.4 基础能力层：可选 brainstem / infra

该层可包含：

- cosmos-star
- Dapr / service mesh / event bus
- 节点发现
- 长连接与后台服务协调

但在设计上必须明确：**它是增强层，不是当前单机版 agent 的启动前提。**

在没有 Dapr 运行时的场景下（如在资源受限的嵌入式设备或完全离线的边缘节点上运行时），框架会自动退化与降级：
- **存储**：从 Dapr State Store 降级为本地内存存储 (`MemoryStore`) 或轻量级嵌入式 KV 存储（如 `BadgerDB`）。
- **通信**：从 Dapr Pub/Sub 降级为本地进程内的 Go Channels 或局域网内的轻量级 NATS 消息队列。
- **编排**：由基于 Dapr Workflow 的分布式持久化执行，降级为进程内直接基于 Go Ticker/Channels 的高频战术小脑循环 (`CerebellumNode`)。

这不仅保障了协议的向后兼容，还消除了 Dapr Sidecar 的网络和资源开销，提供极低的时延和 100% 的离线自治能力。

## 3. 目标请求生命周期

1. **Guard / Reflex**
   - 输入校验
   - 安全策略
   - 静态命令快路径
2. **Context / Memory**
   - 会话状态挂载
   - 历史任务与偏好读取
   - 候选 Skill 检索
3. **Planning**
   - 意图识别
   - 任务拆解
   - 风险评估
   - 执行计划
4. **Execution**
   - 调用 Tool
   - 打开或驱动 Plugin
   - 触发确认点
5. **Review / Commit**
   - 输出结果
   - 记录反馈
   - 经验更新

## 4. 神经网络概念在 agent 中的合理映射

如果要引入“神经网络”这个概念，建议不要把它当成纯营销比喻，而是映射到下面几类真实机制：

### 4.1 作为表示学习层（Representation Layer）

含义：

- 把用户意图、上下文、技能、反馈编码成向量空间
- 支撑语义检索、相似任务召回、偏好匹配

它代表的是 **agent 的语义感知能力**。

### 4.2 作为策略网络（Policy Layer）

含义：

- 预测下一步最适合调用哪个 Skill / Tool / Plugin
- 对候选路径排序

它代表的是 **agent 的路由与决策偏好**。

### 4.3 作为价值评估器（Value / Critic）

含义：

- 评估一个计划是否高风险、低收益、容易失败
- 在执行前给出审阅优先级

它代表的是 **agent 的风险判断与自我校验能力**。

### 4.4 作为记忆压缩器（Memory Compression）

含义：

- 将历史执行轨迹压缩成可重用经验
- 形成“偏好”“反例”“最佳实践”的嵌入表示

它代表的是 **从经验中泛化**，而不是简单日志堆积。

## 5. 不建议的做法

以下做法会让“神经网络”概念失真：

- 把每个模块机械命名成神经元但没有协议边界
- 用“神经网络”替代正式的数据结构与服务契约
- 把规则系统、缓存系统、插件系统都笼统称作神经网络

更好的做法是：

- 神经网络只代表 **学习型路由 / 表示 / 评估模块**
- 系统架构仍然通过明确的 service contract、schema、permission model 来定义
