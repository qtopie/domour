# Module Spec: LlamaCpp Runtime Library (`ark/infra/llamacpp`)

## 1. Overview

The `ark/infra/llamacpp` package provides a lightweight, thread-safe Go runtime library around `llama.cpp` within **Ark (Agent Runtime Kit)**.

### Architectural Boundaries & Principles:
1. **Low-Level Compute & Tensor Foundation Only**:
   - Focuses strictly on model loading, single-forward-pass logits extraction (`ForwardLogits`), and autoregressive token generation (`Generate` / `Stream`).
   - Does **NOT** contain System-1 or System-2 cognitive abstractions (e.g. `Question`, `Choice`, `Score`, `Boolean`, softmax normalization, decision state machines). All cognitive and decision logic belongs to the consuming layer (e.g., `rezrov`).
2. **On-Demand Consumption**:
   - Packaged as an embeddable library in `ark/infra/llamacpp`.
   - Core server daemons in `domour` do not import or depend on `llamacpp` by default, eliminating tight coupling.
3. **Thread Safety & Worker Serialization**:
   - `llama.cpp` contexts are single-threaded for evaluation. The library manages concurrency via an internal serialized worker queue (`chan inferenceJob`).
4. **Conditional In-Tree CGO Isolation**:
   - In-tree minimal CGO bridge (`bridge.h`, `bridge.c`, `runner_cgo.go`) is compiled only under `//go:build llamacpp`. It links directly against native `libllama.so` and exposes only primitive scalar/pointer ABI functions, avoiding external third-party wrapper dependencies (e.g. `gollama.cpp`).
   - In default builds (`//go:build !llamacpp`), a deterministic pure-Go stub/simulated runner is provided, ensuring `go test ./...` and CI environments pass with zero CGO dependencies.
5. **CLI Regression Tooling**:
   - `cmd/main.go` exposes `domour llamacpp` to verify and regression-test model loading, logits extraction, and generation from the command line.

---

## 2. Interface / API Contract

### 2.1 Engine Interface (`ark/infra/llamacpp`)

```go
package llamacpp

type Engine interface {
    // ForwardLogits executes a single forward pass over input tokens and returns raw logits
    // for the specified marker indices (e.g., [MASK] token positions).
    ForwardLogits(ctx context.Context, req ForwardRequest) (*ForwardResponse, error)

    // Generate produces text completions synchronously.
    Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)

    // Stream generates text asynchronously, streaming tokens over a channel.
    Stream(ctx context.Context, req GenerateRequest) (<-chan TokenChunk, error)

    // Chat executes multi-turn conversational generation synchronously.
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

    // ChatStream executes multi-turn conversational generation asynchronously with token streaming.
    ChatStream(ctx context.Context, req ChatRequest) (<-chan TokenChunk, error)

    // IsReady reports whether the model and context are ready for inference.
    IsReady() bool

    // Close releases model context and stops worker goroutines.
    Close() error
}

type Config struct {
    ModelPath string `json:"model_path"`
    NCtx      uint32 `json:"n_ctx"`
    NThreads  int32  `json:"n_threads"`
    UseStub   bool   `json:"use_stub"`
}

type ForwardRequest struct {
    Prompt        string  `json:"prompt,omitempty"`
    Tokens        []int32 `json:"tokens,omitempty"`
    MarkerIndices []int   `json:"marker_indices"` // indices within tokens where logits are extracted
}

type ForwardResponse struct {
    TokensCount int                 `json:"tokens_count"`
    Logits      map[int][]float32   `json:"logits"` // key: marker index, value: vocab logits slice
}

type GenerateRequest struct {
    Prompt      string   `json:"prompt"`
    MaxTokens   int      `json:"max_tokens"`
    Temperature float32  `json:"temperature"`
    TopP        float32  `json:"top_p,omitempty"`
    Stop        []string `json:"stop,omitempty"`
}

type GenerateResponse struct {
    Content          string `json:"content"`
    PromptTokens     int    `json:"prompt_tokens"`
    CompletionTokens int    `json:"completion_tokens"`
    FinishReason     string `json:"finish_reason"`
}

type ChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type ChatRequest struct {
    Messages    []ChatMessage `json:"messages"`
    MaxTokens   int           `json:"max_tokens"`
    Temperature float32       `json:"temperature"`
    TopP        float32       `json:"top_p,omitempty"`
    Stop        []string      `json:"stop,omitempty"`
}

type ChatResponse struct {
    Message          ChatMessage `json:"message"`
    PromptTokens     int         `json:"prompt_tokens"`
    CompletionTokens int         `json:"completion_tokens"`
    FinishReason     string      `json:"finish_reason"`
}

type TokenChunk struct {
    Text    string `json:"text"`
    TokenID int32  `json:"token_id"`
    Done    bool   `json:"done"`
    Err     error  `json:"-"`
}
```

### 2.2 CLI Interface (`cmd/`)

```bash
# Run self-check / smoke regression test
domour llamacpp smoke

# Run generation regression test
domour llamacpp generate --prompt="Hello world" [--model=/path/to/model.gguf]

# Run single forward logits extraction test
domour llamacpp forward --prompt="The sky is [MASK]" --markers=3 [--model=/path/to/model.gguf]
```

---

## 3. Acceptance Criteria (BDD)

### Feature: Single-Forward Pass Logits Interception

#### Scenario 1: [SPEC-LLAMA-001] Forward logits extraction at specified marker positions
- **Given** an initialized `llamacpp.Engine`
- **When** `ForwardLogits` is called with input tokens (or prompt) and a list of `MarkerIndices`
- **Then** the engine executes a single forward pass (without autoregressive decoding), captures logits for each requested marker index, and returns `ForwardResponse` containing logits slices for those positions.
- **Mapped Test:** `ark/infra/llamacpp/engine_test.go:TestEngine_ForwardLogits`

#### Scenario 2: [SPEC-LLAMA-002] Forward logits validation on invalid marker index
- **Given** an initialized `llamacpp.Engine`
- **When** `ForwardLogits` is called with an out-of-bounds marker index (e.g. index >= token count)
- **Then** the engine returns an error indicating invalid marker index without crashing.
- **Mapped Test:** `ark/infra/llamacpp/engine_test.go:TestEngine_ForwardLogits_InvalidMarker`

---

### Feature: Autoregressive Text Generation & Streaming

#### Scenario 3: [SPEC-LLAMA-003] Synchronous generation and streaming token emission
- **Given** an initialized `llamacpp.Engine`
- **When** `Generate` is called with a prompt, and `Stream` is called with a prompt
- **Then** `Generate` returns a `GenerateResponse` with complete content and token usage; `Stream` emits `TokenChunk` items terminating with `Done: true`.
- **Mapped Test:** `ark/infra/llamacpp/engine_test.go:TestEngine_GenerateAndStream`

---

### Feature: Thread-Safe Worker & Cancellation

#### Scenario 4: [SPEC-LLAMA-004] Serialized inference queue and cancellation handling
- **Given** an active `llamacpp.Engine` with an internal worker queue
- **When** multiple goroutines concurrently invoke `ForwardLogits` and `Generate`
- **Then** inference tasks are processed serially without data races; cancelled requests exit cleanly with `ctx.Err()`.
- **Mapped Test:** `ark/infra/llamacpp/worker_test.go:TestWorker_ConcurrencyAndCancellation`

---

### Feature: CLI Regression Tool

#### Scenario 5: [SPEC-LLAMA-005] CLI llamacpp sub-command execution for regression testing
- **Given** the `domour` CLI binary or main entry point
- **When** `domour llamacpp smoke` or `domour llamacpp generate` is executed
- **Then** the CLI initializes the engine, performs the requested inference test, outputs results, and exits with code 0.
- **Mapped Test:** `ark/infra/llamacpp/cli_test.go:TestCLI_LlamaCpp`

---

### Feature: Multi-Turn Chat Generation & Streaming Stop Words

#### Scenario 6: [SPEC-LLAMA-006] Multi-turn Chat conversation and stop sequence truncation
- **Given** an initialized `llamacpp.Engine`
- **When** `Chat` is invoked with multi-turn messages and a list of stop words (e.g. `["<|im_end|>"]`)
- **Then** the engine formats messages with ChatML / Jinja template, streams or evaluates tokens, truncates upon detecting any stop sequence, and returns a `ChatResponse` containing the assistant message without the stop token.
- **Mapped Test:** `ark/infra/llamacpp/engine_test.go:TestEngine_Chat`

#### Scenario 7: [SPEC-LLAMA-007] ChatStream with multi-byte UTF-8 buffering
- **Given** an initialized `llamacpp.Engine`
- **When** `ChatStream` is invoked
- **Then** tokens are streamed incrementally without breaking multi-byte UTF-8 characters across chunks, terminating with `Done: true`.
- **Mapped Test:** `ark/infra/llamacpp/engine_test.go:TestEngine_ChatStream`

---

## 4. Notes & Non-Goals

- **Non-goal**: Cognitive decision logic, Question/Choice/Boolean mapping, and entropy math belong to `rezrov` and are strictly excluded from this package.
- **Non-goal**: Modifying default domour ACP server behavior.
- **Build Tag Separation**: When built without `-tags llamacpp`, the library uses a high-fidelity stub runner so unit tests run cleanly anywhere.
