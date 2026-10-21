# RFC-0005: Unified ChatGPT & Codex Provider for Agent Supervisor

- **Status:** Under Review (Pending User Approval)
- **Author:** Domour Agent Architecture Team
- **Created Date:** 2026-10-21
- **Target Spec:** `specs/modules/agent-supervisor.spec.md` (SPEC-SUPV-016 ~ SPEC-SUPV-019)

---

## 1. Summary

本提案在 `ark/supervisor` 现有多 Agent 架构基础之上，设计并实现针对 **ChatGPT / Codex CLI**（基于官方 `@openai/codex` 运行时与底层原生 Rust 引擎）的统一适配器 `ark/supervisor/providers/chatgpt`。

本 Provider 将统一收敛对 ChatGPT 和 Codex 的支持，通过：
1. **统一别名注册**：主名称为 `chatgpt`，同时别名注册 `codex` 与 `openai`，共享同一套底层抽象；
2. **零侵入 SQLite 数据库与进程自动发现**：通过解析 `~/.codex/state_5.sqlite` 的 `threads` 表与系统进程信息，快速提取本地活跃会话、标题、Tokens 消耗与工作区路径；
3. **结构化单步事件流与会话回溯 (JSONL / Inspect)**：适配 `codex exec --json` 输出的事件流，以及直接解析历史 rollout 日志，映射为通用 `TranscriptRecord`；
4. **标准 JSON-RPC 2.0 通信 (App-Server)**：封装 `codex app-server` 的协议接口（`thread/start`, `turn/start`, `thread/read`, `approval` 拦截等），对齐 Antigravity 的 `agentapi` 双向控制能力。

---

## 2. Motivation & Background

在上一阶段，Domour 深度支持了 Antigravity (`agy`)，验证了“进程发现 + 结构化日志监听 + 遥测控制”体系的高可靠性。
通过对当前系统环境的实测勘探，我们发现：
1. 本地已完整安装了 OpenAI 官方 Codex CLI (`@openai/codex` v0.147.0)，其底层调用原生 Rust 二进制，并内置了 `~/.codex/state_5.sqlite` 数据库与 `threads` 会话持久化机制；
2. 其具备与 Antigravity `agentapi` 完全对标（甚至更加标准化）的 `app-server` JSON-RPC 2.0 协议层，包含了对 ChatGPT 认证（`ChatgptAuthTokensRefreshParams`）、多轮对话（`thread/*`, `turn/*`）以及高危命令/补丁审批（`approval`）的完整规范；
3. 用户期望像 Antigravity 一样，将 ChatGPT 和 Codex 合并为一个统一的载体，并以 `chatgpt` 作为首选接入名称。

---

## 3. Detailed Architecture

### 3.1 目录结构与模块定位

```
ark/supervisor/providers/chatgpt/
├── provider.go       // AgentProvider 核心实现 (实现 Name, BuildHeadlessArgs, MatchSession, ReadRecentTurns 等)
├── discover.go       // 基于 ~/.codex/state_5.sqlite 与系统进程的自动发现
├── transcript.go     // 解析 codex JSONL 结构化事件流 -> supervisor.TranscriptRecord
├── rpc.go            // app-server JSON-RPC 2.0 客户端封装 (thread/start, turn/start, approval)
└── types.go          // Codex/ChatGPT 专用数据模型 (Threads, Rollout, JSON-RPC 结构体)
```

### 3.2 别名注册与智能路由 (Provider Registry)

在 `ark/supervisor/registry.go` 中，统一注册：
- `chatgpt` (主 Provider)
- `codex` (别名，指向 `chatgpt`)
- `openai` (别名，指向 `chatgpt`)

无论用户通过 `-p chatgpt` 还是 `-p codex` 均能无缝调度同一底层实现。

### 3.3 零侵入本地会话发现 (Discovery & Inspect)

1. **数据库读取**：
   - 路径：`$HOME/.codex/state_5.sqlite`
   - 查询 `threads` 表：获取 `id`, `cwd`, `title`, `model`, `tokens_used`, `created_at`, `rollout_path`。
2. **活跃进程关联**：
   - 扫描 Linux `/proc` 目录中的 `codex` 或 `app-server` 进程 PID，将持久化状态与当前活跃进程结合。
3. **回溯多轮会话**：
   - 读取并解析 `rollout_path`（或执行 `thread/read` RPC），转换为标准的 `[]*TranscriptRecord`。

### 3.4 流式运行与 Headless 控制

1. **单次/流式执行 (CLI Headless)**：
   - 命令行：`codex exec --json --cd <workspace> -m <model> [PROMPT]`
   - 参数根据模式自适应：
     - 只读对话（ChatGPT 体验）：默认 `--sandbox read-only` 或 `--ask-for-approval untrusted`
     - 编码写入（Codex 体验）：配置 `--sandbox workspace-write`
2. **事件流转换**：
   - 实时将标准输出的 JSONL 事件映射为 `TranscriptRecord`，反馈至终端或 WebSocket。

---

## 4. Verification & Testing Strategy

1. **Harness 夹具**：
   - `harness/fixtures/supervisor/chatgpt/threads.sqlite`：提供标准的 SQLite 数据库样例；
   - `harness/fixtures/supervisor/chatgpt/exec_events.jsonl`：包含思考、工具调用与应答的样例事件流。
2. **单元测试与 Spec 校验**：
   - `testings/supervisor/chatgpt_test.go`：覆盖 `SPEC-SUPV-016` ~ `SPEC-SUPV-019`；
   - 运行 `./scripts/check.sh` 确保零回归与零 Spec Drift。
