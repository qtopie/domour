# Domour 项目开发路线图 (TODO List)

## 阶段一：基础模型与通信协议 (已完成或进行中)
- [x] 定义多用户对话助手接口 (`chat.proto`)：支持会话分支、合并请求、状态追踪
- [x] 定义无声辅助助手接口 (`assist.proto`)：支持上下文同步、异步建议、反馈闭环
- [x] 定义全自动守护助手接口 (`autopilot.proto`)：支持长期任务目标、工具执行回调、人类在环授权拦截
- [x] 确立跨语言 RPC 通信层：基于 gRPC + Protobuf + 双向 Stream 流
- [x] 确立 `domour` 核心引擎模块架构：大脑 (Brain) / 小脑 (Cerebellum) / 脑干 (Brainstem) 物理层级划分

## 阶段二：大脑模块 (Brain) 核心实现
- [ ] **LLM 驱动接入 (`pkg/core/brain/llm`)**
  - [ ] 实现 Gemini API 原生接入 (支持 Function Calling 约束封装)
  - [ ] 实现多模型适配器扩展 (OpenAI/Anthropic 格式等兼容接口)
- [ ] **多级记忆引擎 (`pkg/core/brain/memory`)**
  - [ ] 实现短期上下文记忆栈 (Session-based Memory)
  - [ ] 实现长期记忆对接机制 (基于 Vector DB 的语义召回，如 Qdrant/Milvus/SurrealDB)
- [ ] **主控意识体 (`pkg/core/brain/conscious`)**
  - [ ] 编写 Daemon 后台循环，实现自我状态定时健康检查和任务反思 (Reflection)
- [ ] **确定性规则分析 (`pkg/core/brain/rule`)**
  - [ ] 挂载正则表达式或脚本规则引擎 (处理必须严格控制的特定命令)

## 阶段三：小脑模块 (Cerebellum) 逻辑编排实现
- [ ] **规划编排器 (`pkg/core/cerebellum/orchestrator`)**
  - [ ] 实现大意图拆解系统 (Plan -> Subtasks DAG)
  - [ ] 构建 ReAct 闭环循环 (Thought -> Act -> Observe 工具流)
- [ ] **能力定义引擎 (Skill & Tools)**
  - [ ] 解析和加载自定义 `skill.md` 文件生成 JSON Schema 工具声明
  - [ ] 集成 MCP (Model Context Protocol) 框架支持
  - [ ] 完成本地基础能力的装载 (FileSystem、HTTPRequest等)

## 阶段四：脑干外挂 (Brainstem / Cosmos-star DI 注入)
- [ ] **依赖注入框架配置 (`internal/app/`)**
  - [ ] 打通 `domour` 向下对 `cosmos-star` 组件注入的生命周期 (`brainstem.go`)
- [ ] **通信设施接管**
  - [ ] 对接 `cosmos-star` 的 NATS / MQTT 事件总线 (`EventBus` 实现)
- [ ] **任务下发队列**
  - [ ] 在 `cosmos-star` 侧实现 `TaskDispatcher`：能够把包装好的长任务下发给其它边缘节点执行，并异步拿回 Result

## 阶段五：上层助手服务实现 (Service Layer)
- [ ] **ChatService (应用层)**：使用编排好的小脑，接入客户端多路复用长连接
- [ ] **AssistService (应用层)**：实现 `ContextSync` 和 `UserAction` 分析，高频返回推断和 `Command`
- [ ] **AutopilotService (应用层)**：接通自动化后台任务和 `RequireHumanApproval` (授权推送拦截) 流程

## 阶段六：测试、观测与打磨
- [ ] **构建单元测试沙箱**：Mock LLM 返回和 Mock Node 执行进行覆盖率测试
- [ ] **全链路可观测性 (Observability)**：为每一此脑路反思(PlanDecomposition)接入 Trace 日志
- [ ] **边缘部署适配**：将整个项目连带底层的 `cosmos-star` 交叉编译发布至终端设备
