# domour

## MVP server

`domour` 现在默认启动一个最小可运行的 gRPC agent MVP，不依赖插件、SurrealDB、NATS 或本地配置文件。

启动：

```bash
go run ./cmd/domour
```

默认监听：

```bash
127.0.0.1:1234
```
也可以通过环境变量覆盖：

```bash
DOMOUR_ADDRESS=127.0.0.1:2234 go run ./cmd/domour
```

## Available entrypoints

MVP 默认注册 3 个服务入口：

- `assistant.copilot.CopilotService/Copilot`
- `assistant.chat.ChatService/Chat`
- `assistant.autopilot.AutopilotService/Autopilot`

它们共用一个内建最小 agent：

- `Chat`：当前走 agent 内部双 goroutine 编排：brain goroutine 把事件写入 `SessionBridge`，motor goroutine 从 `SessionBridge` 读取并统一对外流式返回；普通对话由 brain 产出文本，图类请求由 brain 产出 D2 计划，再由 motor 统一拦截、渲染或拒绝。对图片消息，motor 现在会并行跑一层轻量 OCR/事实提取拦截，并在 brain 开始生成前尽量把这层上下文注入进去
- `Copilot`：支持两种模式；积极模式走 `agent -> brain(stream) -> motor`，普通模式走 `agent -> motor -> brain -> motor`
- `Autopilot`：当前走 `agent -> motor -> brain -> motor`；motor 会先筛简单任务并直接返回，复杂任务再旁路请求 brain 产出计划，最后仍由 motor 统一返回

## Notes

- 当前实现保留了 session history 的内存版存储
- 该 MVP 旨在为 `cosmos-assistant` 和后续 Skill / Plugin 架构提供可运行入口
- 更复杂的插件式 copilot、外部基础设施和 brain 模块可以在此基础上逐步接回
- Brain 的目标集群设计见 `docs/brain.md`；核心原则是 Brain 只负责生成中间结果，Motor 持有最终输出、工具执行和安全裁决权
- 模型调用现在统一经由 `internal/pkg/brain/diencephalon`（间脑）转发，`brain`/`agent` 只依赖这层接口；底层 provider 适配仍由 `internal/pkg/brain/llm` 提供，支持 `ollama`、`gemini-cli`、`github-copilot-cli`、`qodercli`，`copilot-cli` 仍保留为兼容别名
- 三个入口可以分别通过环境变量选择 provider/model：

```bash
DOMOUR_DEFAULT_PROVIDER=github-copilot-cli
DOMOUR_DEFAULT_MODEL=gpt-5

DOMOUR_CHAT_PROVIDER=github-copilot-cli
DOMOUR_COPILOT_PROVIDER=github-copilot-cli
DOMOUR_AUTOPILOT_PROVIDER=github-copilot-cli
```

- 也支持 OpenAI-compatible 本地后端，例如 **Ollama**：

```bash
DOMOUR_DEFAULT_PROVIDER=ollama
DOMOUR_DEFAULT_MODEL=phi4-mini
DOMOUR_DEFAULT_BASE_URL=http://127.0.0.1:11434/v1
DOMOUR_DEFAULT_API_KEY=ollama
```

- `DOMOUR_{ENTRY}_BASE_URL` / `DOMOUR_DEFAULT_BASE_URL` 与 `DOMOUR_{ENTRY}_API_KEY` / `DOMOUR_DEFAULT_API_KEY` 现在也可用于按入口或全局覆盖模型后端；`ollama` 默认会回退到 `http://127.0.0.1:11434/v1`
- 图片聊天的 OCR/上下文拦截优先复用本地 `ollama` 配置；也可以单独用 `DOMOUR_OCR_PROVIDER`、`DOMOUR_OCR_MODEL`、`DOMOUR_OCR_BASE_URL`、`DOMOUR_OCR_API_KEY` 覆盖

- 如果不设置分入口 provider，三个入口都会默认继承 `DOMOUR_DEFAULT_PROVIDER`；最终默认值是 `github-copilot-cli`
- `Copilot` 模式选择优先读消息内联标记，其次读 `DOMOUR_COPILOT_MODE`：

```bash
DOMOUR_COPILOT_MODE=active
```

- 也支持在消息里显式覆盖：

```text
[active] explain this handler
[normal] rename this function safely
/active please suggest the patch
/normal summarize the change
```

- Domour 会读取 `~/.domour/config.json`，当前默认会自动生成一个示例配置，并把 `https_proxy` 设为 `http://127.0.0.1:8118`
- `~/.domour/config.json` 也支持 `providers.<name>.base_url` / `providers.<name>.api_key` / `services.brain.mode` / `services.brain.app_id` / `services.motor.mode` / `dapr.grpc_address` / `dapr.http_address`
- 第一阶段 Dapr 适配已经接入 **brain client**：当 `services.brain.mode=dapr` 时，Domour 会通过 Dapr sidecar 的 HTTP invocation 调用远端 brain；当前 `motor` 仍建议保持 `local`
- 进程现在还会启动一个内部 brain HTTP listener（默认 `127.0.0.1:18080`，可用 `DOMOUR_INTERNAL_HTTP_ADDRESS` 覆盖），供 Dapr sidecar 调用本地 brain 能力
- 现在也支持基础的模型管理命令：

```bash
go run ./cmd/domour models list
go run ./cmd/domour models list -provider ollama
go run ./cmd/domour models set -provider ollama -model phi4-mini
go run ./cmd/domour models set -entry chat -provider ollama -model qwen2.5-coder
```

- `models list` 会优先走 provider 原生探测；当前内建支持：
  - `ollama`：读取 `/api/tags`
  - `openai` / OpenAI-compatible：读取 `/v1/models`
  - `gemini`：读取 Google models REST API
  - `github-copilot-cli` / `qodercli`：当前仅支持手动管理，不支持自动列模型
- `brain` 现在支持基础多模态输入：`chat` / `copilot` / `autopilot` 请求都可以附带 `attachments`；当前先支持 **文本 + 图片**，音频和视频保留到后续版本
- 图片聊天现在也支持基础 **OCR** 能力：当用户明确要求 OCR / 提取文字 / 识别图中文字时，brain 会切到更偏文字抽取的提示，优先按阅读顺序返回可见文本
- 图片多模态是否可用取决于底层 provider：`openai` / OpenAI-compatible、`gemini` 等支持多模态输入的后端可直接接收图片；CLI 类 provider 目前会对图片/音频/视频输入显式报错，避免静默降级
- 探测结果和默认模型选择会写回 `~/.domour/config.json`；除 `providers.<name>.base_url` / `api_key` 外，现在还支持：
  - `default_provider`
  - `default_model`
  - `providers.<name>.model`
  - `providers.<name>.models`
  - `entries.chat|copilot|autopilot.provider`
  - `entries.chat|copilot|autopilot.model`
- 内部 brain HTTP 也暴露了模型管理接口：`/internal/brain/models/discover` 与 `/internal/brain/models/set`；如果 brain 通过 Dapr 暴露，这两个接口也可以随 Dapr invocation 一起使用

## License

GPL-3.0
