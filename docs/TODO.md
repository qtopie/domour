# Domour 项目开发路线图 (TODO List)

## 阶段一：基础模型与通信协议 (已完成)
- [x] 定义多用户对话助手接口 (`chat.proto`)：支持会话分支、合并请求、状态追踪
- [x] ~~定义无声辅助助手接口 (`assist.proto`)~~ (已废弃，并入 Copilot 统一处理)
- [x] 定义全自动守护助手接口 (`autopilot.proto`)：支持长期任务目标、工具执行回调、人类在环授权拦截
- [x] 确立跨语言 RPC 通信层：基于 gRPC + Protobuf + 双向 Stream 流
- [x] 确立 `domour` 核心引擎模块架构：大脑 (Brain) / 小脑 (Cerebellum) / 脑干 (Brainstem) 物理层级划分
- [x] 完成仿生架构与计算机科学术语映射：Cerebrum (Inference Engine), Cerebellum (Orchestrator), Brainstem (Gateway)

## 阶段二：大脑模块 (Brain / Engine) 核心实现
- [x] **LLM 驱动接入 (`internal/infra/llm`)**
  - [x] 实现 Gemini API 原生接入 (支持 Function Calling 约束封装)
  - [x] 实现 DeepSeek 原生接入与测试 (OpenAI 兼容模式)
  - [x] 支持多模型适配器 (Llama.cpp, Qwen, Claude-ready)
- [x] **系统运行模式矩阵 (`internal/engine/state.go`)**
  - [x] 实现 Hibernate, Casual, Balanced, Performance 基础模式
  - [x] 实现 Vigilant, Survival, Deep Think, Stealth, Diagnostic 进阶模式
  - [x] 定义多级算力 (Cognitive) 与能量 (Bionic) 分配策略
- [ ] **多级记忆引擎 (`internal/bionic/session`)**
  - [x] 实现短期上下文记忆栈 (Session-based Memory)
  - [ ] 实现长期记忆对接机制 (基于 Vector DB 的语义召回，如 SurrealDB/Qdrant)
- [ ] **核心认知能力 (`internal/engine/cerebrum.go`)**
  - [ ] 完善任务拆解编排逻辑 (Planning)
  - [ ] 完善自我反思与状态自省机制 (Reflection)

## 阶段三：小脑模块 (Cerebellum / Reasoning) 逻辑编排实现
- [ ] **规划编排器 (`internal/reasoning/planner`)**
  - [ ] 实现大意图拆解系统 (Plan -> Subtasks DAG)
  - [x] 构建基础 ReAct 闭环循环 (Thought -> Act -> Observe 工具流)
- [ ] **能力定义引擎 (Skill & Tools)**
  - [ ] 解析和加载自定义 `skill.md` 文件生成 JSON Schema 工具声明
  - [ ] 集成 MCP (Model Context Protocol) 框架支持
  - [x] 完成本地基础能力的装载 (FileSystem, Shell, RenderD2)
- [ ] **外部 Agent 委派与拦截 (Claude Code / Gemini CLI / GitHub Copilot CLI)**
  - [ ] 实现对 GitHub Copilot CLI 的支持与工具调用拦截


## 阶段四：脑干外挂 (Brainstem / Motor / Cosmos-star 接入)
- [x] **高并发调度模型 (`docs/brain.md`)**
  - [x] 设计 Per-Request-of-Goroutine 调度机制
  - [x] 定义多级神经分流机制 (Layered Routing)
- [ ] **依赖注入与通信设施**
  - [ ] 打通 `domour` 向下对 `cosmos-star` 组件注入的生命周期
  - [x] 对接 NATS / MQTT 事件总线 (`internal/infra/bus`)
- [ ] **任务下发队列**
  - [ ] 在 `cosmos-star` 侧实现 `TaskDispatcher`：将包装好的长任务下发给边缘节点执行

## 阶段五：上层助手服务实现 (Service Layer)
- [ ] **ChatService (应用层)**：使用编排好的小脑，接入客户端多路复用长连接
- [ ] **CopilotService (应用层)**：支持人类在环 (Human-in-the-loop) 的代码重构与 Review
- [ ] **AutopilotService (应用层)**：接通自动化后台任务和授权推送拦截流程

## 阶段六：测试、观测与打磨
- [x] **集成测试沙箱 (`docs/architecture/vproxy_integration.md`)**
  - [x] 实现 vproxy 透明代理集成测试，验证 Agent 复杂网络环境适应力
- [ ] **全链路可观测性 (Observability)**
  - [ ] 接入 `slog` + `otel.Tracer` 对每一此脑路反思进行分布式追踪
- [ ] **边缘部署适配**：将整个项目连带底层的 `cosmos-star` 交叉编译发布至 RISC-V/ARM64 终端设备
