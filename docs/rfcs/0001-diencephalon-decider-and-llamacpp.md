# RFC-0001: Diencephalon Cross-Encoder Inference & llama.cpp Logits Interception for System-1/2 Runtime

- **Status:** Approved
- **Author:** Antigravity (rezrov & domour collaborator)
- **Created Date:** 2026-09-25

---

## 1. Summary

本 RFC 提案为 `domour` 的**间脑（Diencephalon / 模型与认知中枢）**引入双模式推理与张量级拦截支持：
1. **System-2 (自回归生成)**：集成 `llama.cpp` (GGUF) 本地连续生成通道（支持流式 SSE 和打字机 Chunk 输出）；
2. **System-1 (单次前向决策 / Cross-Encoder)**：为 ModernBERT-large 及 Qwen-3.5 决策模型提供 **Non-Autoregressive Forward Pass**，在隐藏层直接提取指定标记（如 `[MASK]` 位置）的未归一化分值（Logits），并通过统一 REST 端点 `POST /v1/decide` 向上游（如 `rezrov` 引擎）暴露。

---

## 2. Motivation & Architecture Separation

- **现状与痛点：**
  - 当前 `rezrov` 项目定位为纯粹的认知逻辑与确定性状态机框架（Pure Go），但原先在内部直接引入 CGO `gollama.cpp` 导致跨平台构建复杂、动态链接库沉重；
  - 传统的大模型推理服务（如标准 OpenAI 接口、标准 llama-server）只支持自回归文本吐字（`completions` / `chat`），**无法按指定 Token 位置拦截并抽取最后一层 Logits**。这使得类似 OpenJev / ModernBERT 的单次前向高速决策（10~50ms 意图分类与打分）无法直接走通用服务。
- **职责划分：**
  - **`domour`（算力底座）**：集中管理硬件加速（CUDA/Metal/CPU BLAS）、CGO 依赖、GGUF 权重加载、KV Cache、Slot 队列，以及**核心张量/Logits 抽取操作**；
  - **`rezrov`（认知大脑）**：保持 100% Pure Go，无 CGO 依赖。负责构建带有 `[MASK]` 标记的 Token 序列，调用 `domour` 接口获取 Logits，并在内存中完成温度归一化、信息熵置信度计算与状态机闭环。

---

## 3. Detailed Design

### 3.1 协议契约：`POST /v1/decide`

`domour` 提供标准 HTTP REST 端点，供 `rezrov` 的 `decider.HTTPClient` 调用。

#### Request Payload
```json
{
  "model": "qwen3.5-4b-decision",
  "state": "The user is in a locked control room.",
  "questions": {
    "action": {
      "instructions": "Given user input, which action best matches intent?",
      "option_list": ["open door", "wait patrol", "smash window"]
    },
    "safe": {
      "instructions": "Is the current situation safe?"
    },
    "urgency": {
      "instructions": "How urgent is this event?",
      "criteria": ["low", "medium", "high", "critical"]
    }
  }
}
```

#### Response Payload
```json
{
  "model": "qwen3.5-4b-decision",
  "choices": {
    "action": {
      "type": "choice",
      "choice": "open door",
      "probabilities": {
        "open door": 0.92,
        "wait patrol": 0.05,
        "smash window": 0.03
      },
      "confidence": 0.89
    }
  },
  "booleans": {
    "safe": {
      "type": "boolean",
      "probability": 0.12
    }
  },
  "scores": {
    "urgency": {
      "type": "score",
      "score": 2.7,
      "legend": {
        "0": "low",
        "1": "medium",
        "2": "high",
        "3": "critical"
      },
      "probabilities": {
        "0": 0.05,
        "1": 0.15,
        "2": 0.45,
        "3": 0.35
      },
      "confidence": 0.81
    }
  },
  "usage": {
    "input_tokens": 64,
    "output_tokens": 0
  }
}
```

---

### 3.2 底层 Logits 抽取机制 (llama.cpp / ModernBERT)

对于支持 System-1 判别的底层引擎，关键在于：**单次前向（Single Forward Pass）+ 指定位置 Logits 提取**。

#### 伪代码实现原理 (`llama.cpp` CGO 层面)：
```c
// 1. 将包含 [MASK] 或候选词的 Prompt 打包为一个 batch (单次前向，不循环生成)
llama_batch batch = llama_batch_get_one(tokens, n_tokens);

// 2. 将需要输出 logits 的 token 位置标记 logits[i] = true
for (int i = 0; i < n_markers; i++) {
    batch.logits[marker_indices[i]] = true;
}

// 3. 执行单次评估
llama_decode(ctx, batch);

// 4. 抽取各 marker 位置对应的 logits 向量
for (int i = 0; i < n_markers; i++) {
    float *token_logits = llama_get_logits_ith(ctx, marker_indices[i]);
    // 拷贝并保存候选选项分值...
}
```

---

### 3.3 目录与组件规划（在 `domour` 内部）

建议在 `domour` 中按以下结构落地：

```
domour/
├── internal/
│   ├── diencephalon/       # 间脑 (模型推理与路由)
│   │   ├── engine.go       # 统一接口: Generate(ctx, req) & Decide(ctx, req)
│   │   ├── llamacpp/       # llama.cpp CGO 串行调度器与 Worker 池
│   │   │   ├── runner.go   # 模型载入、单次前向、Logits 抓取
│   │   │   └── bindings.go # CGO 调用绑定
│   │   └── server/         # HTTP REST 网关 (提供 /v1/chat/completions 和 /v1/decide)
```

---

## 4. Alternatives Considered

1. **在 `rezrov` 内部直接调用 CGO**：
   - *未采用原因*：违背了 `rezrov` 作为轻量通用库的定位，破坏跨平台秒级交叉编译能力，使上层应用引入沉重的构建负担。
2. **纯靠大模型自回归输出 JSON 做决策**：
   - *未采用原因*：自回归吐字耗时长（数百毫秒至数秒），且存在幻觉与 JSON 解析失败风险。无法达到 System-1 亚 50ms 极速、强校准置信度的要求。

---

## 5. Security & Performance Considerations

- **性能目标**：单次 `Decide` 请求在 CPU 端（ModernBERT 或 Qwen-0.5B/4B 量化版）延迟控制在 **10ms ~ 50ms**；
- **并发控制**：`domour` 内部的 `llamacpp` 必须使用带缓冲的 Worker 队列（如 `chan InferenceJob`）串行或受控并发访问 Context，防止 GPU/内存竞态；
- **解耦优势**：`rezrov` 与 `domour` 之间解耦后，`domour` 可部署在本地 IPC（Unix Domain Socket / Localhost HTTP）或边缘节点（如 Milk-V Duo S、SpaceMIT K1），架构适应性更强。
