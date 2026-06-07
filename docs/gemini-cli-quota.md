# Gemini CLI 额度与模型发现机制

本文档记录了 `gemini-cli` 获取模型配额及预览版模型访问权限的技术原理，作为 `domour` 后端实现 API 探活的理论依据。

## 核心原理

`gemini-cli` 的模型发现和额度查询并不是基于公开的公共接口，而是通过 Google Cloud Code Assist 的内部 API 实现的。核心在于 **Project ID (项目 ID)** 的隔离与授权。

### 1. 为什么需要 Project ID
在 Google 的账户体系中，即使是个人用户，其配额也是挂载在一个“隐形项目”上的。
- **预览版权限**：`gemini-3.1-pro-preview` 等模型仅在特定项目下授权。如果不传 Project ID，API 仅返回基础模型池。
- **精准配额**：不同项目拥有独立的 RPM (每分钟请求数) 和 RPD (每日请求数) 限制。

### 2. 获取 Project ID 的流程
Project ID 是动态获取的，流程如下：
1. **加载账户信息**：通过 OAuth 认证后的 `access_token` 调用 `loadCodeAssist` 接口。
2. **提取标识符**：从响应正文的 `cloudaicompanionProject` 字段获取真实的 Project ID（例如 `healthy-stack-k0xt0`）。

### 3. 额度查询逻辑
获取 Project ID 后，通过 `retrieveUserQuota` 接口并携带 `{"project": "YOUR_PROJECT_ID"}` 载荷，即可拉取完整的模型桶（Buckets）信息，包括：
- 模型 ID (modelId)
- 剩余比例 (remainingFraction)
- 重置时间 (resetTime)

---

## API 实现方案

在 `domour` 项目中，该逻辑封装在 `internal/core/llm/gemini_api.go` 中。

### 接口概览
1. **`loadCodeAssist`**
   - **Endpoint**: `https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist`
   - **Method**: `POST`
   - **Payload**: `{"mode": "HEALTH_CHECK"}`
2. **`retrieveUserQuota`**
   - **Endpoint**: `https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota`
   - **Method**: `POST`
   - **Payload**: `{"project": "..."}`

### 数据结构
- **GeminiQuotaBucket**: 单个模型的配额快照。
- **GeminiAPIHealth**: 包含平均响应时间 (AvgRT)、账户订阅层级 (Tier) 和完整的模型配额列表。

---

## 注意事项
- **认证依赖**：必须存在有效的 `~/.gemini/oauth_creds.json`。
- **被动刷新**：为了节省 API 调用，当正常的对话请求成功时，应重置探活计时器。
- **平均 RT**：由 `loadCodeAssist` 和 `retrieveUserQuota` 两个并发请求的响应时间求平均值得出。
