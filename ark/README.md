# Ark (Agent Runtime Kit)

`ark` 是 Domour 的公共 SDK 层，为外部集成提供标准化的接口与契约。它通过职责分离的设计，将资源管理、系统治理与引导启动解耦。

## 目录结构与包职责

### [ark](./ark.go)
**用途**：**统一网关 (Unified Gateway)**
作为 SDK 的主入口，通过组合模式聚合了 `hub` 和 `governor` 的能力。它是调用方与 Domour 运行时交互的单一接触点。

### [ark/governor](./governor/governor.go)
**用途**：**治理中心 (Governance Center)**
负责 Domour 的“软状态”调节。
- **模式切换**：管理 `SystemMode`（如休眠、深思、性能模式），调节认知能效与 I/O 策略。
- **看板状态**：提供对 `brain.State` 的全局读写能力，包括任务进度、复杂度分析与用户指令反馈。
- **目标管理**：设定与更新智能体的全局目标 (Global Goal)。

### [ark/telemetry](./telemetry/telemetry.go)
**用途**：**可观测性中心 (Telemetry & Observability)**
负责配置系统的监控与追踪能力。
- **链路追踪 (Tracing)**：配置 OpenTelemetry TracerProvider，支持 OTLP 和 Stdout 导出。
- **日志集成 (Logging)**：集成 `slog` 并确保与追踪上下文关联。
- **指标收集 (Metrics)**：为系统性能监控提供基础架构支持。

### [ark/hub](./hub/hub.go)
**用途**：**资源中心 (Resource Hub)**
负责 Domour “静态能力”的注册与发现。
- **工具管理 (Tools)**：注册与枚举底层的原子执行单元（如 MCP 工具）。
- **技能管理 (Skills)**：加载与管理基于 Markdown 定义的高级 Agent 技能。
- **服务商管理 (Providers)**：管理底层的 LLM Provider 配置与模型路由策略。

### [ark/bootstrap](./bootstrap/server.go)
**用途**：**引导与注入 (Bootstrap & DI)**
负责将 Domour 的各个组件（Brain, Motor, Hub, Governor 等）按照六边形架构进行组装与启动。
- **依赖注入**：连接适配器 (Adapters) 与端口 (Ports)。
- **生命周期管理**：控制 Assistant 服务的启动、运行与优雅停机。

---

## 设计哲学

1. **组合优于继承**：通过 `Ark` 接口组合 `ArkHub` 与 `Governor`，保持接口的简洁与可扩展性。
2. **静态与动态分离**：`hub` 处理静态注册（能力），`governor` 处理动态调节（状态），职责边界清晰。
3. **屏蔽内部细节**：`ark` 目录下的所有接口均通过 `internal/` 包实现，确保护实现在私有空间，仅暴露必要的契约给用户。
