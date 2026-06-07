# Domour Neural Channel Timeout & Anomaly Mitigation Design

This document details the design of the neural signal routing system in `domour`, focusing on context-based timeouts, ring-buffer-based telemetry queue handling, and mitigation strategies for handling system anomalies where channel data goes unprocessed.

---

## 1. Current Design Overview

The `domour` runtime coordinates communication between four primary biomorphic neural component nodes:
- **Diencephalon (间脑)**: Input sensory classification and model gateway.
- **Cerebrum (大脑)**: Cognitive reasoning and task planning (System 2 slow thinking).
- **Cerebellum (小脑)**: Tactical orchestration, tool execution, and high-frequency (1kHz) error correction.
- **Brainstem (脑干)**: Fundamental motor execution and Pons-based signal copy/routing.

These nodes communicate via Go channels as synapses, with the routing logic implemented in [routeNeuroSignals](file:///home/qtopierw/workspace/projects/domour/internal/engine/core.go#L104-L170).

---

## 2. Supporting Timeout Mechanisms

To support a robust timeout mechanism:

### A. Context Propagation in Payloads
We must extend [SensorySignal](file:///home/qtopierw/workspace/projects/domour/internal/brain/brain.go#L80) and other neural structures to propagate the request-scoped `context.Context`. **Note:** Standard deadlines are avoided here as they are deemed unreliable for long-running chain executions; carrying the request `context` allows the system to respond immediately to caller-initiated cancellation.

```go
type SensorySignal struct {
	Ctx       context.Context // Propagated context
	Source    string
	Data      interface{}
	Timestamp time.Time
}
```

### B. Timeout Selects on Channel Write
Instead of indefinitely blocking when writing to internal channels, writes must select on both the request context and a local write limit (cancel/abort if it blocks for too long):

```go
select {
case e.cerebrum.TaskIn <- task:
	// Write succeeded
case <-task.Ctx.Done():
	// Request context cancelled or timed out
case <-time.After(WriteTimeoutLimit):
	// Return failure/error and abort operation
}
```

---

## 3. Handling Unprocessed Channel Data & LLM Failures

If an external LLM call fails, or if a timeout/error causes downstream channels to completely block, the system applies the following mechanisms:

### 1. High-Frequency Data: Ring Buffer
For high-frequency telemetry and sensor signals, a simple "non-blocking drop" might lose critical latest frames. Instead, we use a **Ring Buffer** for the telemetry queues (e.g., `TelemetryIn`).
* When the buffer is full, the oldest data is evicted, ensuring that the Cerebellum always has access to the most up-to-date physical state without blocking the routing loop.

### 2. LLM Call Failures & Retry Policies
When invoking an external LLM:
* **Retry Limit**: The system permits **exactly 1 retry** upon failure.
* **Circuit Breaking (Health Status)**: If failures occur repeatedly beyond the threshold, the LLM proxy wrapper will mark the specific model as unhealthy/inactive to prevent further blocking calls.
* **Timeout and Cancellation**: A strict execution timeout is enforced. If a call takes too long or fails repeatedly, the operation is cancelled and an error is returned.

### 3. Channel Blocking and Fallbacks
If downstream channels are completely blocked because of external LLM timeouts or continuous errors:
* **Default Fail-Fast**: The system will immediately **return a failure** by default.
* **Regulation/Correction**: If a retry/correction is necessary, the system will not try to unblock via local retry loops; instead, it relies on the **Diencephalon's upward regulation/correction loop** (sending feedback via the Diencephalon to re-trigger or adjust the task state dynamically).
