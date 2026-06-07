package governor

import (
	"context"
	"testing"

	"github.com/qtopie/domour/internal/brain"
)

func TestGovernor(t *testing.T) {
	g := NewGovernor()
	ctx := context.Background()

	t.Run("Initial State", func(t *testing.T) {
		mode, err := g.GetMode(ctx)
		if err != nil {
			t.Fatalf("GetMode failed: %v", err)
		}
		if mode != brain.ModeBalanced {
			t.Errorf("expected initial mode to be Balanced, got %q", mode)
		}

		state, err := g.GetState(ctx)
		if err != nil {
			t.Fatalf("GetState failed: %v", err)
		}
		if state.Mode != brain.ModeBalanced {
			t.Errorf("expected state mode to be Balanced, got %q", state.Mode)
		}
	})

	t.Run("Switch Mode", func(t *testing.T) {
		err := g.SwitchMode(ctx, brain.ModePerformance)
		if err != nil {
			t.Fatalf("SwitchMode failed: %v", err)
		}

		mode, _ := g.GetMode(ctx)
		if mode != brain.ModePerformance {
			t.Errorf("expected mode to be Performance, got %q", mode)
		}
	})

	t.Run("Update State", func(t *testing.T) {
		newState := &brain.State{
			GlobalGoal: "Solve world hunger",
			Mode:       brain.ModeDeepThink,
		}
		err := g.UpdateState(ctx, newState)
		if err != nil {
			t.Fatalf("UpdateState failed: %v", err)
		}

		state, _ := g.GetState(ctx)
		if state.GlobalGoal != "Solve world hunger" || state.Mode != brain.ModeDeepThink {
			t.Errorf("unexpected state: %+v", state)
		}
	})

	t.Run("Set Global Goal", func(t *testing.T) {
		err := g.SetGlobalGoal(ctx, "Write more code")
		if err != nil {
			t.Fatalf("SetGlobalGoal failed: %v", err)
		}

		state, _ := g.GetState(ctx)
		if state.GlobalGoal != "Write more code" {
			t.Errorf("expected goal 'Write more code', got %q", state.GlobalGoal)
		}
	})
}
