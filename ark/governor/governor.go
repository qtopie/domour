// Package governor handles the high-level system governance and state regulation
// of the Domour agent runtime.
package governor

import (
	"context"
	"sync"

	"github.com/qtopie/domour/internal/brain"
)

// Governor represents the high-level governance center of Domour.
// It manages the global system state and operating modes, separating
// high-level policy from low-level resource management.
type Governor interface {
	// GetState returns the current global context dashboard of the brain.
	GetState(ctx context.Context) (*brain.State, error)

	// UpdateState updates the global context dashboard with new data.
	UpdateState(ctx context.Context, state *brain.State) error

	// GetMode returns the current system operating mode (e.g., Balanced, DeepThink).
	GetMode(ctx context.Context) (brain.SystemMode, error)

	// SwitchMode changes the system operating mode, regulating the
	// balance between cognitive power (LLM) and bionic energy (I/O).
	SwitchMode(ctx context.Context, mode brain.SystemMode) error

	// SetGlobalGoal updates the high-level goal the system is currently pursuing.
	SetGlobalGoal(ctx context.Context, goal string) error
}

type governor struct {
	mu    sync.RWMutex
	state brain.State
}

// NewGovernor constructs a new Governor instance with default balanced settings.
func NewGovernor() Governor {
	return &governor{
		state: brain.State{
			Mode: brain.ModeBalanced, // Default to balanced mode
		},
	}
}

// GetState retrieves the complete brain state context.
func (g *governor) GetState(ctx context.Context) (*brain.State, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Return a shallow copy to prevent external mutation of internal state
	s := g.state
	return &s, nil
}

// UpdateState overwrites the current global brain state.
func (g *governor) UpdateState(ctx context.Context, state *brain.State) error {
	if state == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state = *state
	return nil
}

// GetMode returns the currently active SystemMode.
func (g *governor) GetMode(ctx context.Context) (brain.SystemMode, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.state.Mode, nil
}

// SwitchMode transitions the system to a specific operating mode.
func (g *governor) SwitchMode(ctx context.Context, mode brain.SystemMode) error {
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
