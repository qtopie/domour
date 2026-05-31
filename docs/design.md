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

---

## 5. 项目核心架构：三层仿生学架构 (Brain - Cerebellum - Brainstem)

为了实现高内聚、低耦合的代理系统，我们参考生物学将本项目的基础设施分为三大层级，明确切分了**“认知推断”**、**“逻辑编排”**与**“物理调度”**的边界。

### 5.1 大脑 (Brain): 认知与推断中枢
- **职责**: 负责思考、记忆、计算、推理，决定“做什么”(What to do)。
- **组成**: 
  - `llm`: 各种大语言模型驱动，负责非确定的模糊推理。
  - `rule`: 规则引擎，负责处理确定性强、高频、需绝对精确的预设逻辑。
  - `memory`: 多级记忆结构（短期对话上下文，长期向量记忆池）。
  - `conscious`: 主控意识体 (Daemon loop)，持续自省、反思并反馈整体操作系统的表现。
- **输入输出**: 接收环境的观察 (Observation) 与事件，输出高层意图 (Intent) 或抽象计划 (Plan)。

### 5.2 小脑 (Cerebellum): 任务流编排与逻辑执行
- **职责**: 接收大脑下发的意图，负责具体战术的编排，决定“怎么做”(How to do)。
- **组成**:
  - `orchestrator`: 任务逻辑编排器 (如 ReAct, Plan-and-Solve 循环)。
  - `tools`: 工具注册与挂载、逻辑层面的接口调用管理等。
- **功能**: 把大脑的高层意图转化为具体的 API 或系统方法调用顺序串，获取返回结果并执行下一步或向上报告。

### 5.3 脑干 (Brainstem): 分布式物理调度与生命维持
- **职责**: 负责计算资源的分配、网络通讯、底层存储，维护 Agent 生死状态，决定“在哪做”(Where to do)。
- **架构隔离**: 在 `domour` 中仅定义 `brainstem` 的 Interface (依赖倒置)。真正的分布式网络、节点监控、物理任务派发由基础支撑项目 `cosmos-star` 去实现与注入。
- **包含的核心概念**:
  - 集群节点与设备状态监控 (`ClusterMonitor` / 软硬件存活探测)。
  - 分布式编排与下发 (`TaskDispatcher` / 将物理任务投递给边缘节点)。
  - 全局事件总线 (`EventBus` / 网络通信)。
  - 兜底持久化与缓存支撑 (`PersistentStorage`)。

---

## 6. 项目最新重构与会话查询设计 (2026-05 重构里程碑)

为了彻底根治项目中后期出现的目录重名、Server 职责冲突及双重架构混乱（“精神分裂”），我们于 2026 年 5 月底进行了一次深度的架构理顺与重大功能升级。

### 6.1 “内核开源，业务私有” 的全新目录蓝图

我们重新划定了 **“内核（Core）对外公用，业务编排（App/Agent）对内私有”** 的边界，彻底拆解并消除了具有误导性的 `internal/pkg/` 双重重名路径：

```text
.
├── proto/               # 契约：定义最纯净、无重复的三大服务 (Chat / Copilot / Autopilot)
├── gen/                 # 自动生成：纯净编译的 gRPC & Protobuf 目标 Go 代码
├── cmd/                 # 编译入口：清理了 .bak 等历史残留，保留 domour 和 domour-chat
│
├── pkg/                 # 【核心内核】供外部插件、边缘节点或内部引用，不包含任何私有业务
│   └── core/            
│       ├── brain/       # 基础大脑接口、状态与观察定义
│       ├── brainstem/   # 脑干控制
│       ├── cerebellum/  # 小脑（协调/记忆/运动）
│       ├── llm/         # 统一的大模型客户端内核 (Ollama/Gemini/Claude API & CLI 等)
│       ├── stem/        # 【原 internal/pkg/stem】信号入口过滤器
│       ├── motor/       # 【原 internal/pkg/motor】物理执行与工具执行核心
│       └── skill/       # 【原 internal/pkg/skill】Skill Markdown 解析核心
│
└── internal/            # 【私有业务】Domour 核心业务，外部不可引用，避免依赖循环
    ├── bootstrap/       # 【原 internal/pkg/bootstrap】gRPC & HTTP 服务端启动器
    ├── app/             # 应用层管理 (config, modelmanager)
    ├── session/         # 会话生命周期与 SurrealDB/Memory 持久化管理
    └── agent/           # 【原 internal/pkg/agent】Agent 具体业务编排与控制流
        ├── shared/      # 跨组件共享的 Request/Message 模型
        ├── diencephalon/# 间脑：上层 Agent 与底层 llm 驱动的动态转发路由器
        ├── mvp/         # 兜底规则脑 (Diagram/Copilot 逻辑容错防线)
        ├── observer/    # 复杂度评估 Eino 节点
        ├── planner/     # 计划生成 Eino 节点
        ├── react/       # ReAct 模式 Eino 节点
        ├── simple/      # 简单对话处理 Eino 节点
        └── worker/      # 原子执行 Eino 节点
```

通过此重构，循环导入的风险直接降为零，依赖方向变为绝对单向：
$$\text{cmd} \rightarrow \text{internal (业务/传输)} \rightarrow \text{pkg/core (内核契约)} \rightarrow \text{gen (gRPC/Protobuf)}$$

### 6.2 统一会话查询与延迟加载缓存 (Lazy-Load & Cache) 设计

随着不同 LLM Provider（特别是 CLI 本地命令行工具如 `gemini`、`agy` 等）的使用，会话数据出现了“云端（DB）”和“本地（CLI Log）”割裂的痛点。为此，我们设计并落地了**统一会话查询与延迟加载缓存服务**：

#### 1. 统一会话查询 (`QuerySessions`)
* **多源归并**：查询服务首先从 SurrealDB 或 MemoryStore 中读取在线对话会话，随后自动递归扫描本地 CLI 的历史文件目录（`~/.gemini/tmp/workspace/chats/*.jsonl` 和 `~/.antigravity/...`）。
* **CLI 日志清洗**：能够提取 `.jsonl` 增量日志的会话 ID、模型、最近活跃时间，并利用正则表达式自动脱敏 `[SYSTEM]` 提示词以呈现纯净的对话末尾摘要。
* **全局时间线排序**：去重合并后，统一按 `UpdatedAt` 降序排列。
* **终端集成**：提供了 `domour sessions list` 统一查询子命令，支持 `-provider` 过滤与 `-json` 输出。

#### 2. 会话延迟加载与自动缓存 (`Lazy-Load & Cache`)
* **痛点**：CLI 产生的会话仅保存在本地磁盘的 `.jsonl` 中，当用户通过 gRPC 连接想继续该会话时，服务端由于数据库没有该记录，会将其当作新会话处理，导致历史丢失。
* **解决方案**：
  * 在 `internal/agent/server.go` 的 `getSession` 中加入拦截探针。
  * 如果在 active DB store 中找不到对应的 SessionID 记录，服务端会自动调用 `QuerySessions` 探测本地 CLI 缓存。
  * 发现匹配的本地 CLI 日志后，自动逆向解析出历史 Message 数组和模型参数，**将其写入 SurrealDB / 内存数据库进行动态缓存初始化**。
  * 后续的对话将在此缓存基础上继续追加并持久化，实现了 **“本地 CLI 调试会话 ➡️ 网络 gRPC 续写对话”** 的无缝连接。

