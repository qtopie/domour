# 当前架构（Current State）

本文档描述仓库 **当前已经存在** 的实现，不代表未来目标架构。

## 1. 运行时组成

当前应用由 3 个已经落地的部分组成：

1. **Wails 桌面宿主 (`cosmos-assistant`)**
   - 提供主窗口、菜单、设置、本地文件与系统集成功能。
   - `App` 作为 Wails service 暴露前端可调用方法。
2. **插件宿主 (`PluginManager`)**
   - 通过 `/plugins` 路由向前端提供插件静态资源。
   - 通过 HashiCorp `go-plugin` + gRPC 加载独立进程插件。
3. **Domour 接入层**
   - 应用启动时尝试启动 Domour。
   - 前端聊天能力通过 `App.Chat()` 连接本地 Domour gRPC 服务。

## 2. 当前真实边界

### 2.1 主程序

- 入口位于 `main.go`。
- 当前注册了两个 Wails service：
  - `App`
  - `PluginManager`（服务名 `PluginManager`，HTTP 路由 `/plugins`）

### 2.2 插件系统

- 插件以 **独立二进制** 形式存在。
- 插件前端资源通过 `/plugins/<plugin-id>/assets/...` 暴露。
- 插件后端能力通过 `window.PluginManager.invoke(pluginID, method, args)` 由前端回调到宿主，再转发到插件进程。
- 插件当前 manifest 仅包含：
  - `id`
  - `name`
  - `version`
  - `entrypoint`
  - `methods`

这说明当前插件协议更接近 **UI plugin/runtime contract**，还不是完整的 Skill 协议。

### 2.3 Domour 集成

- `App.ServiceStartup()` 会异步调用 `startServices()`。
- `startServices()` 当前会启动 Domour bootstrap。
- `App.Chat(message)` 通过 gRPC 连接 `localhost:9526` 的 Domour Copilot 服务。

### 2.4 cosmos-star 集成现状

- cosmos-star **当前不是稳定的内嵌组件**。
- 代码已明确说明：旧的 embedded Cosmos-Star bootstrap API 已变更，当前启动路径被禁用。
- `QueryTask()` 仍保留了通过 Dapr 调用 `cosmos-star` 的接入方式，但它依赖外部服务可用。

因此，当前应把 cosmos-star 视为：

- **外部可选能力**
- **非启动必需依赖**
- **尚未完成稳定嵌入的集成点**

## 3. 当前数据流

### 3.1 聊天请求

`Frontend -> Wails App.Chat() -> Domour gRPC -> response stream -> Frontend`

### 3.2 插件资源加载

`Frontend iframe -> /plugins/<plugin-id>/assets/... -> PluginManager -> plugin asset`

### 3.3 插件方法调用

`Frontend window.PluginManager.invoke() -> Wails binding -> PluginManager.Invoke() -> go-plugin gRPC -> plugin`

## 4. 当前架构的约束

1. **Skill 与 Plugin 还未解耦**
   - 当前只有插件协议，没有正式的 Skill registry / skill schema / routing schema。
2. **插件沙箱尚未成型**
   - 目前插件通过 iframe 嵌入，但尚未形成完整的 sandbox / permission / bridge 白名单模型。
3. **cosmos-star 还不能作为基础层前提**
   - 当前设计不能假设它总是嵌入、可启动、可调度。
4. **反馈学习仍是愿景**
   - 还没有经验存储、偏好写入、负反馈约束、回放策略的正式协议。

## 5. 当前设计决策

在新一轮设计中，以下内容应视为已确认现实：

- `cosmos-assistant` 是桌面宿主与产品壳
- `PluginManager` 是当前最成熟的扩展机制
- Domour 是当前主要智能入口
- cosmos-star 在现阶段只能被建模为可选外部能力，而不是默认脑干层
