# domour

`domour` 是一个 **分布式仿生智能体运行时 (Distributed Bionic Agent Runtime)**。

它为 AI Agent 提供了一个解耦、安全且可扩展的“生命支持系统”。不仅仅是一个 SDK，`domour` 更是一个让智能体能够“思考 (Think)”与“执行 (Act)”的标准化环境，支持从嵌入式边缘节点到云端集群的无缝部署。

## 核心定位：Agent Runtime

- **运行环境 (Runtime)**: 提供智能体的生命周期管理、状态持久化、多级记忆管理和跨节点通讯。
- **仿生架构 (Bionic Architecture)**: 严格遵循“大脑 (Brain) - 小脑 (Motor) - 脑干 (Brainstem)”的解耦设计，实现认知与执行的物理隔离。
- **分布式协同 (Distributed)**: 基于 Dapr 和网格计算思想，支持智能体节点间的智能化协同与任务路由。
- **双模式接入**:
  - **Embedded SDK**: 作为库集成进现有应用，为业务逻辑注入 Agent 能力。
  - **Bootstrap Server**: 快速启动一个标准化的 Agent 节点，作为独立的运行时服务。

---

## 快速启动

`domour` 默认可以启动一个最小可运行的 gRPC agent runtime 实例，无需复杂配置。

启动服务端：

```bash
go run ./cmd/domour
```

默认监听 `127.0.0.1:1234`。

---

## 架构概览

`domour` 的核心是将 Agent 的能力拆分为互不干扰的生理模块：

- **大脑 (Brain)**: 认知中枢。负责语义理解、任务拆解、生成计划。它被视为“非确定性”层。
- **小脑 (Motor)**: 执行中枢。负责工具调用、安全审查、最终输出。它是系统的“确定性”控制层，确保 Brain 的想法安全落地。
- **脑干 (Brainstem)**: 基础设施层。负责节点通信、事件总线、存储调度。基于 Dapr 实现集群节点的发现与状态同步。
- **间脑 (Diencephalon)**: 模型路由层。统一调度 Gemini, Claude, Ollama 以及各类本地 CLI 模型。

## 功能特性

- **跨环境适配**: 无论是笔记本上的本地 LLM (Ollama)，还是云端的高性能 API，亦或是边缘设备的简单规则，都能在同一套 Runtime 下运行。
- **零信任安全**: 认知与执行物理隔离，Brain 无法直接操作敏感资源，必须经过 Motor 的二次校验。
- **统一记忆系统**: 自动归并并持久化会话历史，支持延迟加载本地 CLI 日志，实现 Agent 记忆的无缝平移。
- **多模态原生**: 内置多模态消息处理与轻量化 OCR 拦截，支持附件通道。

## 配置与管理

提供模型管理工具，支持动态切换运行时后端：

```bash
go run ./cmd/domour models list
go run ./cmd/domour models set -entry chat -provider ollama -model qwen2.5-coder
```

## License

This project is licensed under the Mozilla Public License 2.0 - see the [LICENSE](LICENSE) file for details.

Copyright (c) 2026 qtopie. All rights reserved.
