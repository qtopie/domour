# Domour Decoupled Reasoning & Event-Driven Orchestration Design

This document details the architectural design for separating the **Event Bus**, **Reasoning (Cognitive State Machine)**, and **Execution** layers within `domour`, aligning with the framework's biological positioning of Brain (Cerebrum), Thalamus (Diencephalon), Cerebellum, and Brainstem.

---

## 1. Biomorphic Component Mapping

To prevent tight coupling between cognitive planning and event-driven orchestration, the orchestration logic is mapped to biological brain structures according to their true evolutionary roles:

```
                    ┌──────────────────────────────┐
                    │       大脑 (Cerebrum)        │ ──► Macro Cognition & Plan Generation
                    │  【无状态语义推理/模式决策】  │
                    └──────────────▲───────────────┘
                                   │
                                   ▼ (Intents & High-Level Plans)
                    ┌──────────────────────────────┐
                    │    间脑/丘脑 (Diencephalon)   │ ──► Session State & Coordinator (Event Bus)
                    │  【事件总线/会话看板/信号中继】│
                    └──────────────▲───────────────┘
                                   │
                                   ▼ (Sub-task Dispatch)
                    ┌──────────────────────────────┐
                    │      小脑 (Cerebellum)       │ ──► Tactical Execution & Local Loops (ReAct)
                    │  【独立工具调用/自治子推理】  │
                    └──────────────────────────────┘
```

1. **Diencephalon (间脑/丘脑 - Coordinator & Event Bus)**:
   Acts as the central traffic control tower and session coordinator. It manages the `TaskContext`/`State` blackboard for active sessions, consumes synaptic events, runs the active reasoning engine, and routes messages between the other nodes.
2. **Cerebrum (大脑 - Macro Cognition & Planner)**:
   Operates as the System 2 slow thinker. It performs high-level semantic planning (generating steps/Plan List) and reflection. It is stateless and does not micro-manage execution steps.
3. **Cerebellum (小脑 - Tactical Autonomous Executor)**:
   Operates as the System 1 executor. It receives sub-tasks from the Diencephalon. It has the autonomy to decide *how* to execute them, either invoking a single tool directly via the Brainstem or initiating a local ReAct loop of its own before returning the results to the Thalamus.
4. **Brainstem (脑干 - Physical Layer)**:
   Performs system calls and tool actions, acting as the safety veto and final execution layer.

---

## 2. Reasoning Interfaces & Data Contracts

All reasoning algorithms must reside in the [internal/reasoning/](file:///home/qtopierw/workspace/projects/domour/internal/reasoning) directory and implement the `Reasoner` interface, ensuring infinite extensibility without modifying core communication code.

### A. Core Structures

```go
package brain

import (
	"context"
	"time"
)

type EventType string

const (
	EventUserQuery   EventType = "USER_QUERY"
	EventLLMResponse EventType = "LLM_RESPONSE"
	EventExecResult  EventType = "EXEC_RESULT"
	EventError       EventType = "ERROR"
)

// Event represents a message routed through the Thalamus Event Bus.
type Event struct {
	SessionID string
	Type      EventType
	Payload   interface{}
	Timestamp time.Time
}

type NextActionType string

const (
	ActionCallLLM  NextActionType = "CALL_LLM"  // Request Diencephalon/Cerebrum cognitive reasoning
	ActionCallTool NextActionType = "CALL_TOOL" // Dispatched tool/skill call to Cerebellum/Brainstem
	ActionFinish   NextActionType = "FINISH"    // Complete execution and return final MotorFeedback
)

// NextStep determines the next action recommended by the active Reasoner.
type NextStep struct {
	Action  NextActionType
	Payload interface{}
}

// State represents the central session blackboard managed by the Thalamus.
type State struct {
	SessionID  string
	GlobalGoal string
	
	// ReasonerState holds algorithm-specific state context (e.g. Plan steps, Tree search paths)
	ReasonerState map[string]interface{}
	
	History      []Event
	ActiveEngine string // e.g. "simple", "react", "plan_execute", "tot"
}
```

### B. Reasoner Interface

```go
package brain

import "context"

// Reasoner is the interface for all pluggable reasoning algorithms.
type Reasoner interface {
	// Name returns the unique identifier for the reasoning engine.
	Name() string
	
	// Decide is a stateless decision function.
	// Input: current session State blackboard, new incoming Event.
	// Output: the NextStep action to be orchestrated.
	Decide(ctx context.Context, state *State, event Event) (NextStep, error)
}
```

---

## 3. Orchestration Workflow

1. **Sensory Intake**: Sensory signals are submitted via Diencephalon, creating or retrieving the session `State` blackboard.
2. **Reasoner Lookup**: The Diencephalon Coordinator fetches the registered `Reasoner` corresponding to `State.ActiveEngine`.
3. **Decide Loop**: The Coordinator executes `Reasoner.Decide(ctx, state, event)`.
4. **Synaptic Relay**: The Coordinator reads the resulting `NextStep` and forwards the appropriate `Event` to either the Cerebrum (`CerebrumOut` channel), Cerebellum (`CerebellumOut` channel), or external output (`ResponseOut`).
5. **Autonomy & Nesting**:
   * If a sub-task is dispatched to the Cerebellum, the Cerebellum has the autonomy to resolve it locally (potentially using a local Eino ReAct Graph) before returning an `EventExecResult` back to the Thalamus.
   * If plan adjustment is needed due to error, the Diencephalon routes the event to the Cerebrum for macro reflection and re-planning.

### 3.3 Cerebellar Autonomy & Context Protection

* **Context Protection (防污染机制)**: During micro-refinements or local reasoning loops, any intermediate thoughts, trials, or micro-observations generated by the Cerebellum are encapsulated locally inside the Cerebellum Node. They are **not** appended to the Diencephalon's global session history (`State.History`), ensuring that the primary LLM context remains focused and clean.
* **Knee-Jerk Reflex (小脑反射弧)**: The Cerebellum Node maintains a local registry of rules, heuristics, and skills. When a sub-task is dispatched to the Cerebellum, it attempts to resolve it using deterministic local reflexes first. If it cannot, or if a local execution loop times out, it reports the failure upward to the Diencephalon as an event to trigger high-level cognitive reflection in the Cerebrum.

