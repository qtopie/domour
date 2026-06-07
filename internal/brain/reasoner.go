package brain

import (
	"context"
	"sync"
	"time"
)

type EventType string

const (
	EventUserQuery   EventType = "USER_QUERY"
	EventLLMResponse EventType = "LLM_RESPONSE"
	EventExecResult  EventType = "EXEC_RESULT"
	EventError       EventType = "ERROR"
)

// Event is a message routed through the Thalamus Event Bus.
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

// Reasoner is the interface for all pluggable reasoning algorithms.
type Reasoner interface {
	// Name returns the unique identifier for the reasoning engine.
	Name() string

	// Decide is a stateless decision function.
	// Input: current session State blackboard, new incoming Event.
	// Output: the NextStep action to be orchestrated.
	Decide(ctx context.Context, state *State, event Event) (NextStep, error)
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Reasoner)
)

// RegisterReasoner registers a Reasoning Engine.
func RegisterReasoner(name string, r Reasoner) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = r
}

// GetReasoner looks up a registered Reasoning Engine.
func GetReasoner(name string) (Reasoner, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	r, ok := registry[name]
	return r, ok
}
