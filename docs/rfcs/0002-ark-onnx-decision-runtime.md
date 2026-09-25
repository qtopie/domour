# RFC-0002: In-Tree ONNX Runtime Engine for System-1 Cross-Encoder Decision Models

- **Status:** Approved
- **Author:** Antigravity (rezrov & domour collaborator)
- **Created Date:** 2026-09-25

---

## 1. Summary

本 RFC 提案为 `domour` 引入针对 **System-1 判别模型（ModernBERT / Laya / BERT 系列 Cross-Encoder）** 的轻量原生推理运行时：`ark/infra/onnx`。

与已经落地的 `ark/infra/llamacpp`（服务于自回归生成大模型 Qwen）平级并列，`ark/infra/onnx` 专门负责执行 **`model.onnx` 静态计算图**，完成单次前向（Forward Pass）并提取多头分类/掩码打分 Logits，实现端侧 **10ms ~ 20ms** 极速决策。

---

## 2. Motivation

1. **架构互补**：
   - `llamacpp` 擅长跑自回归 Decoder 模型（Qwen3.5-4B / LLaMA3，输出生成式文本）；
   - 但对于基于 ModernBERT-large 架构训练的 Laya 决策判别模型，最原生、最高效、工业界标准格式是 **ONNX**。
2. **摆脱 Python 与臃肿环境**：
   - 如果直接在 Python 中跑 PyTorch `safetensors`，环境动辄几个 GB，无法嵌入边缘节点或独立二进制；
   - 通过在 `domour` 封装 `onnxruntime` C API，仅依赖单个 `libonnxruntime.so` 动态库，即可实现零 Python 依赖的纯编译型高性能决策算力。
3. **上游解耦**：
   - `rezrov` 维持 100% Pure Go，只依赖 `decider.ModelRunner` 接口或通过 HTTP/In-Process 桥接 `domour/ark/infra/onnx`。

---

## 3. Detailed Design

### 3.1 运行时包路径规划 (`ark/infra/onnx`)

参考 `ark/infra/llamacpp` 的成熟成功范式，采用相同的极简 In-Tree 结构：

```
domour/
└── ark/
    └── infra/
        ├── llamacpp/        # Qwen / 自回归大模型 (C-Shim + libllama.so)
        └── onnx/            # ModernBERT / Laya / 决策模型 (C-Shim + libonnxruntime.so)
            ├── types.go     # Config, BatchInput, BatchOutput, Engine 接口定义
            ├── engine.go    # Thread-Safe Worker 任务队列
            ├── bridge.h     # C-Shim 极简头文件 (~60 行)
            ├── bridge.c     # 调用 OrtCreateSession, OrtRun (~100 行)
            ├── runner.go    # 内部 runner 接口
            ├── runner_cgo.go # //go:build onnx (链接 libonnxruntime.so)
            ├── runner_stub.go# //go:build !onnx (Pure-Go 零依赖 Stub)
            └── engine_test.go
```

### 3.2 核心接口定义 (`ark/infra/onnx/types.go`)

与 `rezrov/decider.ModelRunner` 契约精准对齐：

```go
package onnx

import "context"

// Config defines initialization parameters for ONNX session.
type Config struct {
    ModelPath string `json:"model_path"` // e.g. "/path/to/laya_modernbert.onnx"
    NumThreads int   `json:"num_threads"` // 默认 4
    UseStub    bool  `json:"use_stub"`    // 纯 Go 测试桩开关
}

// BatchInput is the input tensor batch passed to ONNX.
type BatchInput struct {
    BatchSize     int     `json:"batch_size"`
    SeqLen        int     `json:"seq_len"`
    MaxMarkers    int     `json:"max_markers"`
    InputIDs      []int64 `json:"input_ids"`      // [BatchSize * SeqLen]
    AttentionMask []int64 `json:"attention_mask"` // [BatchSize * SeqLen]
    MarkerPos     []int64 `json:"marker_pos"`     // [BatchSize * MaxMarkers]
    MarkerMask    []bool  `json:"marker_mask"`    // [BatchSize * MaxMarkers]
    QType         []int64 `json:"q_type"`         // [BatchSize] (0: choice, 1: score, 2: boolean)
}

// BatchOutput contains raw logits from model heads.
type BatchOutput struct {
    Logits   []float32 `json:"logits"`    // [BatchSize * MaxMarkers]
    ActProbs []float32 `json:"act_probs"` // [BatchSize * 2]
}

// Engine defines the ONNX decision engine interface.
type Engine interface {
    Run(ctx context.Context, batch BatchInput) (BatchOutput, error)
    Close() error
}
```

### 3.3 编译与构建标签隔离

遵循与 `llamacpp` 完全一致的策略：
- **默认环境 / CI 单测**：`//go:build !onnx` 自动使用 `stubRunner`，**零外部 C 库依赖，毫秒级通过**；
- **生产环境 / 硬件加速**：启用 `go build -tags onnx`，链接 `libonnxruntime.so`。

---

## 4. 上游 Rezrov 接入规划

在 `rezrov/decider` 中直接构造：
```go
import (
    "github.com/qtopie/domour/ark/infra/onnx"
    "github.com/qtopie/rezrov/decider"
)

// 1. 初始化 domour ONNX 引擎
onnxEng, err := onnx.NewEngine(onnx.Config{
    ModelPath: "/models/laya_modernbert_large.onnx",
    NumThreads: 4,
})

// 2. 注入 rezrov.decider.LocalEngine
decEngine := decider.NewLocalEngine(onnxEng, decider.Config{MaxLen: 512}, specialIDs, tokenizer)

// 3. 执行单次 15ms 极速前向决策
res, err := decEngine.Decide(ctx, state, questions)
```

---

## 5. Security & Performance Considerations

- **性能预期**：ModernBERT-large (约 1.5GB) 在 x86_64 CPU (AVX2) 上执行一次 BatchSize=1, MaxMarkers=4 的前向决策耗时在 **12ms ~ 25ms**；
- **内存占用**：Session 内存驻留约 1.8GB，无动态显存暴涨风险；
- **并发控制**：内部通过任务队列隔离，保证单 Session 内多 Goroutine 调用安全。
