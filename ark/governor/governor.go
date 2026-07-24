// Package governor handles the high-level system governance and state regulation
// of the Domour agent runtime.
package governor

import (
	"context"
	"sync"
)

// SystemMode defines the system operating mode, balancing cognitive power (LLM) and bionic energy (I/O)
type SystemMode string

const (
	ModeHibernate   SystemMode = "hibernate"
	ModeCasual      SystemMode = "casual"
	ModeBalanced    SystemMode = "balanced"
	ModePerformance SystemMode = "performance"
	ModeVigilant    SystemMode = "vigilant"
	ModeSurvival    SystemMode = "survival"
	ModeDeepThink   SystemMode = "deep_think"
	ModeStealth     SystemMode = "stealth"
	ModeDiagnostic  SystemMode = "diagnostic"
)

// TaskStatus defines the lifecycle of a task
type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
)

// TaskStep represents an atomic task step definition
type TaskStep struct {
	ID          string                 `json:"id"`
	Action      string                 `json:"action"`
	Input       map[string]interface{} `json:"input"`
	Status      TaskStatus             `json:"status"`
	Observation string                 `json:"observation"`
}

// State represents the global context dashboard of the governor / brain
type State struct {
	SessionID     string                 `json:"session_id,omitempty"`
	TopicID       string                 `json:"topic_id,omitempty"`
	CurrentTopic  string                 `json:"current_topic,omitempty"`
	GlobalGoal    string                 `json:"global_goal,omitempty"`
	Complexity    int                    `json:"complexity,omitempty"`
	Steps         []*TaskStep            `json:"steps,omitempty"`
	CurrentStepID string                 `json:"current_step_id,omitempty"`
	UserFeedback  string                 `json:"user_feedback,omitempty"`
	Mode          SystemMode             `json:"mode,omitempty"`
	ActiveEngine  string                 `json:"active_engine,omitempty"`
	ToolCallCount int                    `json:"tool_call_count,omitempty"`
	MaxToolCalls  int                    `json:"max_tool_calls,omitempty"`
}

// Governor represents the high-level governance center of Domour.
// It manages the global system state and operating modes, separating
// high-level policy from low-level resource management.
type Governor interface {
	// GetState returns the current global context dashboard of the brain.
	GetState(ctx context.Context) (*State, error)

	// UpdateState updates the global context dashboard with new data.
	UpdateState(ctx context.Context, state *State) error

	// GetMode returns the current system operating mode (e.g., Balanced, DeepThink).
	GetMode(ctx context.Context) (SystemMode, error)

	// SwitchMode changes the system operating mode, regulating the
	// balance between cognitive power (LLM) and bionic energy (I/O).
	SwitchMode(ctx context.Context, mode SystemMode) error

	// SetGlobalGoal updates the high-level goal the system is currently pursuing.
	SetGlobalGoal(ctx context.Context, goal string) error
}

type governor struct {
	mu    sync.RWMutex
	state State
}

// NewGovernor constructs a new Governor instance with default balanced settings.
func NewGovernor() Governor {
	return &governor{
		state: State{
			Mode: ModeBalanced, // Default to balanced mode
		},
	}
}

// GetState retrieves the complete brain state context.
func (g *governor) GetState(ctx context.Context) (*State, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Return a shallow copy to prevent external mutation of internal state
	s := g.state
	return &s, nil
}

// UpdateState overwrites the current global brain state.
func (g *governor) UpdateState(ctx context.Context, state *State) error {
	if state == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state = *state
	return nil
}

// GetMode returns the currently active SystemMode.
func (g *governor) GetMode(ctx context.Context) (SystemMode, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.state.Mode, nil
}

// SwitchMode transitions the system to a specific operating mode.
func (g *governor) SwitchMode(ctx context.Context, mode SystemMode) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.state.Mode = mode
	return nil
}

// SetGlobalGoal updates the primary objective stored in the system state.
func (g *governor) SetGlobalGoal(ctx context.Context, goal string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state.GlobalGoal = goal
	return nil
}

