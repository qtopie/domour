
# 设计要点整理

本文档聚焦三块内容：
1. UI 交互、意图识别、多模态感知与任务编排。
2. AI 服务插件化（AI 模型能力 / SKILL / MCP / Function Call）。
3. LLM Agent 架构要素、自省能力与技能化落地。

---

## 1. 交互与编排

- UI 交互：面向用户的会话与操作入口。
- 意图识别：将用户目标映射为可执行任务。
- 多模态感知：支持文本、图像、语音等输入。
- 任务编排：将任务拆解并串联到可执行流程。

交互形式
* 聊天
* 事件触发
* 任务创建
* 窗口操作


结果输出: 
- markdown文本
- 动态卡片(前端)
- html


---

## 2. AI 服务插件化

目标：对 AI 模型能力、技能（SKILL）、MCP 与 Function Call 做统一的插件化抽象。

### 2.1 插件机制候选

- Caddy module（静态编译）
- HashiCorp go-plugin（gRPC）
- WebAssembly (Wasm)
- Yaegi（动态解释器）

### 2.2 插件方案对比

| 方案 | 实现机制 | 跨平台兼容性 | 性能 | 开发难度 | 适合场景 |
| --- | --- | --- | --- | --- | --- |
| 静态编译 (Caddy 模式) | 源码级 import，通过 `init()` 注册 | 极佳，一份源码到处编译 | 最高（原生调用） | 低（纯 Go） | 核心功能插件（加密协议、基础存储） |
| HashiCorp go-plugin | 主从进程，gRPC 通信 | 优秀，支持 UDS (L/M) 与 TCP (W) | 中（跨进程开销） | 中（需 Protobuf） | 算力密集型插件（LLM 推理后端、复杂爬虫） |
| WebAssembly (Wasm) | 嵌入 wazero，加载 `.wasm` | 极佳，一份二进制到处运行 | 中下（解释/AOT） | 较高（需编译为 Wasm） | 安全敏感或第三方贡献插件 |
| Yaegi | 运行时解析执行 `.go` | 优秀，无需额外环境 | 较低（解释执行） | 极低（直接写 Go） | 高频变动的办公自动化、小工具 |

### 2.3 统一插件接口与按功能注册

目标是“统一插件内核 + 能力注册”，避免一个超大接口：

- 统一内核只负责生命周期、元信息、配置、权限与观测。
- 插件按功能注册能力（capability），由调用方按能力发现并绑定。
- 每个能力只定义最小职责边界，降低耦合与演进成本。

能力注册建议包含：

- 能力标识（如 UI 渲染、规划、自省、向量检索、工具调用、策略校验）。
- 能力输入/输出约束（数据结构与错误语义）。
- 依赖能力声明（需要哪些上游能力或资源）。

收益：

- 插件可以只实现部分能力，组合更灵活。
- 统一内核保持稳定，能力接口可独立演进。
- 便于按场景启用/禁用能力，便于治理。

---

## 3. LLM Agent 架构要素

本次讨论聚焦 LLM Agent 的完整要素，并强调通过自省能力与技能化提升复杂问题解决能力。

### 3.1 完整的 Agent 要素模型

- 交互层 (UI/Session)：维持状态与多模态感知。
- 大脑层 (Brain)：包含 Planning（任务拆解）与 Reflection（自省）。
- 存储层 (Memory)：区分短期上下文与向量库长期记忆。
- 执行层 (Skills/Tools)：通过 API 或代码与外部世界交互。
- 约束与环境 (Policy/Sandbox)：安全边界、预算控制与运行沙箱。

### 3.2 自省能力 (Reflection)

自省是 Agent 的逻辑闭环，包含三层：

- 过程自省 (ReAct)：观察工具结果，不符合预期时调整思考路径。
- 批判自省 (Critique)：生成器-审查器双逻辑，在输出前自检漏洞。
- 结果自省 (Verification)：通过单元测试或交叉验证确保结果可信。

### 3.3 基于 `skill.md` 的技能化落地

- 现状：Gemini API 原生支持 Function Calling（JSON），Gemini CLI 支持读取 `SKILL.md`。
- 落地策略：
	- 解析器模式：将 Markdown 描述与参数转换为 JSON Schema。
	- 指令挂载：把 `skill.md` 约束与示例作为 System Instruction。
- 优势：行为定义（MD）与业务逻辑（代码）解耦，便于快速迭代。

### 3.4 Skill + MCP + Renderer 的全能助手示例

目标：用 Skill 负责理解与约束，用 MCP 工具负责执行，用渲染器负责输出页面。

示例场景（TODO 领域）：

1. 意图识别：判定当前请求属于 TODO（create/list/update）。
2. 加载 Skill：注入 todo skill 的 instructions 和工具约束。
3. 调用 MCP 工具：例如 `todo.create` 或 `todo.list`。
4. 结果整理：将工具结果转换为统一的 ViewModel（如 `TodoListViewModel`）。
5. 页面渲染：将 ViewModel 渲染为 markdown 或 html。

最小协议建议：

- Skill 声明工具与输出模板，减少模型自由发挥。
- MCP 工具返回结构化数据，避免模型猜测字段。
- Renderer 只接受 ViewModel，保证 UI 一致。

示例（概念）：

- `todo.create({title, due, tags}) -> {id, title, status}`
- `todo.list({filter}) -> {items:[...]}`
- `render.todoPage({items, stats}) -> html/markdown`

---

## 4. 下一步行动建议

1. 编写第一个 `skill.md`，定义具体场景与调用协议。
2. 实现自动化加载脚本，将 MD 生成 Gemini `tools` 配置。
3. 构建自省闭环：模型调用工具 -> 获取结果 -> 模型自评 -> 最终回复。
