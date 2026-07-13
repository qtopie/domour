# Domour Rich Stream Output Design (Reasoning, Collaboration & Tool Calls)

This document details the architectural design and protocols used by `domour` to capture and stream the reasoning process, inter-node agent collaboration, and tool executions.

---

## 1. Stream Chunk Types (`ChunkType`)

To provide rich visual rendering in client IDEs, standard stream responses are structured into five categories:

1. **`CHUNK_TEXT`**: User-facing normal output.
2. **`CHUNK_THINKING`**: Reasoning steps, internal monologues, and cognitive reflections (e.g., DeepSeek-R1 `<think>` tags).
3. **`CHUNK_COLLABORATION`**: Control transitions and message relays between agent components (Diencephalon, Cerebrum, Cerebellum, Brainstem).
4. **`CHUNK_TOOL_CALL`**: Tool invocations (parameters, execution status, execution duration, and observation output).

---

## 2. API Data Contracts

### A. gRPC Protocol (`ChatResponse`)
The gRPC streaming `Chat` endpoint returns `ChatResponse` objects containing the following message properties:

```protobuf
message ChatResponse {
  string session_id = 1;
  int32 seq = 2;
  ChunkType type = 3;
  string content = 4;

  ThinkingDetail thinking = 5;
  CollaborationDetail collaboration = 6;
  ToolCallDetail tool_call = 7;

  bool done = 8;
  map<string, string> meta = 9;
}
```

### B. ACP JSON-RPC Progress Notifications
For Stdio transport (JSON-RPC 2.0), the server sends one-way progress notifications during execution:

* **Method**: `notifications/domour/stream_event`
* **Params**:
  ```json
  {
    "sessionId": "session-id-string",
    "type": "thinking | collaboration | tool_call | text",
    "content": "Progress content or raw token",
    "thinking": { "engine": "deepseek-r1", "stage": "plan", "elapsed_ms": 120 },
    "collaboration": { "from_node": "cerebrum", "to_node": "diencephalon", "event_type": "plan_result" },
    "tool_call": { "tool_name": "execute_command", "status": "started", "arguments": "{\"cmd\":\"go test\"}" }
  }
  ```

---

## 3. Underlying Provider Adaptation

Different LLM providers are adapted to ensure uniform stream output format:

* **Llama.cpp**: Captures inline `<think>...</think>` tags using a streaming regex state parser, redirecting those tokens to `CHUNK_THINKING`.
* **DeepSeek API**: Intercepts the raw SSE stream and maps `choices[0].delta.reasoning_content` to `CHUNK_THINKING`.
* **CLI Providers (agy, copilot, etc.)**: Rewritten to run asynchronously (`cmd.Start()`), piping stdout and parsing logs in real-time to stream thoughts, tool calls, and text without blocking the UI.
