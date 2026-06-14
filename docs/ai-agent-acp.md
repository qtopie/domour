# AI Agent ACP 协议：连接 AI 智能体与 IDE 的桥梁

## 1. 什么是 ACP 协议？

**ACP (Agent Client Protocol)**，通常被称为 **Agent Control Protocol** 或 **Agent Client Protocol**，是一个开放标准的通信协议，旨在弥合 AI 编码智能体（AI Coding Agents）与 IDE/编辑器之间的鸿沟。

ACP 对 AI 智能体而言，就像 LSP (Language Server Protocol) 对编程语言一样：它提供了一套通用的“语言”，使得任何兼容 ACP 协议的 AI 智能体都可以无缝接入任何支持该协议的 IDE，而无需为每个 IDE 开发特定的插件。

## 2. 核心架构与原理

ACP 协议基于 **JSON-RPC 2.0** 构建，支持全双工通信。

### 2.1 握手与能力协商

当 ACP 客户端（如 IDE）启动 ACP 服务端（如 Domour Agent）时，首先进行 `initialize` 握手。

*   **InitializeRequest**: 客户端发送其版本信息以及 `ClientCapabilities`（能力集）。
*   **InitializeResult**: 服务端返回其版本信息以及 `ServerCapabilities`，告知客户端它支持哪些功能（如特定的实验性模式、工具调用能力等）。

### 2.2 消息结构

协议遵循标准的 JSON-RPC 2.0 格式：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "1.0.0",
    "capabilities": { ... },
    "clientInfo": { "name": "IntelliJ IDEA", "version": "2026.1" }
  }
}
```

## 3. IDE 与 ACP AI Agent 的协同机制

以 **IntelliJ IDEA** (v2026.1+) 为例，IDE 与 Agent 的协同主要通过以下方式实现：

### 3.1 代理注册与发现 (Bring Your Own Agent)

开发者不再局限于 IDE 厂商提供的 AI 助手。通过本地的 `acp.json` 配置文件或 IDE 内置的 Agent 市场，开发者可以轻松挂载自定义的 AI 智能体。

*   **配置方式**：在 `~/.jetbrains/acp.json` 中定义智能体的路径和启动参数。
*   **注册表**：IDE 启动时会扫描这些配置，并将智能体加载到 AI 聊天面板或右键菜单中。

### 3.2 上下文共享 (Context Sharing)

为了让 Agent 提供准确的代码建议，IDE 会通过 ACP 协议向 Agent 共享深度代码上下文：
*   **文件内容与结构**：当前光标位置、打开的文件列表。
*   **语义分析**：通过 MCP (Model Context Protocol) 整合，IDE 可以向 Agent 提供符号引用、类型定义等深度信息。

### 3.3 结构化指令执行 (Action Execution)

ACP 允许 Agent 不仅仅返回文本，还能向 IDE 发送结构化命令：
*   **代码修改**：应用特定的 Diff 或重构建议。
*   **导航**：让 IDE 跳转到特定文件或符号。
*   **系统调用**：请求执行终端命令、运行测试或打开预览窗口。

### 3.4 AI 自动代码修改：像 LSP 一样重绘开发体验

实现 AI 自动修改代码是 ACP 协议的核心设计初衷之一。其定位非常像 AI 时代的 **LSP (Language Server Protocol)**：
*   **LSP** 解决了“任何编辑器都能支持任何编程语言语法提示”的问题。
*   **ACP** 则解决了“**任何编辑器都能直接调用任何 AI 智能体来读写、修改代码**”的问题。

**交互流程：**
1.  **Agent 生成方案**：AI Agent 理解需求后，生成修改建议。
2.  **返回标准 Diff**：Agent 通过 ACP 定义的标准数据结构向 IDE 发送“修改建议”（通常包含 Diff 差异）。
3.  **IDE 预览与应用**：IDE 接收响应后，为用户渲染 Diff 对比窗口。用户点击 **Accept** 后，由 IDE 安全地将代码写入本地文件。

### 3.5 关键上下文：工作路径 (Workspace Path) 的传递

IDE 绝对会把工作路径（Project Root）发送给 Agent，这是 Agent 建立索引、解析路径以及配合文件系统工具（如 fs.read/write）的基础。主要传递方式包括：
*   **启动阶段环境变量**：IDE 以子进程形式拉起 Agent 时，通常将项目根目录作为工作目录（CWD）。
*   **初始化阶段 (Initialize Request)**：在 `initialize` 请求的 `params` 中，IDE 会包含 `rootUri` 或 `workspaceFolders` 字段，明确告知绝对路径。
*   **动态同步**：支持 `didChangeWorkspaceFolders` 通知，在多模块项目或切换工作区时动态更新路径。

## 4. Domour 项目中的 ACP 实现

在 Domour 框架中，我们在 `ark/acp` 模块中实现了符合规范的 ACP 服务。

### 4.1 实验性能力支持

Domour 扩展了 ACP 的能力集，引入了 `domourMode`：
*   **Proxy 模式**：作为代理层（vproxy），将 ACP 指令转发给其他执行组件。
*   **Cognitive 模式**：直接利用 Domour 的大脑（Brain）层进行逻辑推理和任务规划，并将结果通过 ACP 反馈给 IDE。

### 4.2 运行模式

Domour 支持通过命令行启动 ACP 服务：
```bash
domour acp
```
该命令会启动一个基于标准输入输出（Stdio）的 ACP 传输层，供 IDE 进程进行父子进程通信。

## 5. ACP 的意义与未来

1.  **消除厂商锁定**：开发者可以根据任务需求，在同一个编辑器中自由切换 Claude、Copilot、Gemini 或自定义的本地 Agent。
2.  **降低开发成本**：智能体开发者只需实现一次 ACP 规范，即可在 IntelliJ、Zed、VS Code 等多个平台运行。
3.  **生态融合**：ACP 与 MCP、LSP 等协议相互配合，将构建出一个更加开放、可插拔的 AI 辅助开发生态系统。
