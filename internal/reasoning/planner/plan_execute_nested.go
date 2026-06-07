package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/qtopie/domour/internal/brain"
)

// PlanExecuteNestedReasoner implements the brain.Reasoner interface.
// It generates a plan containing sub-tasks that may require autonomous Cerebellar loops.
type PlanExecuteNestedReasoner struct{}

func init() {
	brain.RegisterReasoner("plan_execute_nested", &PlanExecuteNestedReasoner{})
}

// Name returns the identifier of the reasoner.
func (r *PlanExecuteNestedReasoner) Name() string {
	return "plan_execute_nested"
}

// Decide processes session events and coordinates the Nested Plan & Execute loops.
func (r *PlanExecuteNestedReasoner) Decide(ctx context.Context, state *brain.State, event brain.Event) (brain.NextStep, error) {
	if state.ReasonerState == nil {
		state.ReasonerState = make(map[string]interface{})
	}

	switch event.Type {
	case brain.EventUserQuery:
		query := event.Payload.(string)
		state.GlobalGoal = query
		return brain.NextStep{
			Action:  brain.ActionCallLLM,
			Payload: fmt.Sprintf("generate_nested_plan: %s", query),
		}, nil

	case brain.EventLLMResponse:
		respText := event.Payload.(string)

		var rawLines = strings.Split(respText, "\n")
		// Skip first line if it's the intent
		if len(rawLines) > 0 {
			firstLine := strings.TrimSpace(rawLines[0])
			if firstLine == "plan" || firstLine == "execute_workflow" || firstLine == "respond" {
				rawLines = rawLines[1:]
			}
		}

		var steps []string
		for _, rawLine := range rawLines {
			trimmed := strings.TrimSpace(rawLine)
			if trimmed != "" {
				trimmed = strings.TrimPrefix(trimmed, "- ")
				trimmed = strings.TrimPrefix(trimmed, "* ")
				if idx := strings.Index(trimmed, ". "); idx != -1 && idx < 5 {
					trimmed = trimmed[idx+2:]
				}
				steps = append(steps, trimmed)
			}
		}

		// Fallback nested steps if parsing yielded empty results
		if len(steps) == 0 {
			steps = []string{"react:inspect_env", "trigger_math_calculators"}
		}

		state.ReasonerState["steps"] = steps
		state.ReasonerState["current_index"] = 0

		state.Steps = make([]*brain.TaskStep, len(steps))
		for i, s := range steps {
			state.Steps[i] = &brain.TaskStep{
				ID:     fmt.Sprintf("step-%d", i),
				Action: s,
				Status: brain.StatusPending,
			}
		}

		firstStep := steps[0]
		state.Steps[0].Status = brain.StatusRunning
		state.CurrentStepID = state.Steps[0].ID

		return brain.NextStep{
			Action:  brain.ActionCallTool,
			Payload: firstStep,
		}, nil

	case brain.EventExecResult:
		stepsRaw := state.ReasonerState["steps"]
		if stepsRaw == nil {
			return brain.NextStep{}, fmt.Errorf("no steps initialized in plan_execute_nested reasoner")
		}
		steps := stepsRaw.([]string)

		currIdxRaw := state.ReasonerState["current_index"]
		if currIdxRaw == nil {
			currIdxRaw = 0
		}
		currIdx := currIdxRaw.(int)

		if currIdx < len(state.Steps) {
			state.Steps[currIdx].Status = brain.StatusCompleted
			if payloadStr, ok := event.Payload.(string); ok {
				state.Steps[currIdx].Observation = payloadStr
			}
		}

		nextIdx := currIdx + 1
		state.ReasonerState["current_index"] = nextIdx

		if nextIdx < len(steps) {
			nextStep := steps[nextIdx]
			state.Steps[nextIdx].Status = brain.StatusRunning
			state.CurrentStepID = state.Steps[nextIdx].ID

			return brain.NextStep{
				Action:  brain.ActionCallTool,
				Payload: nextStep,
			}, nil
		}

		var summaryBuilder strings.Builder
		summaryBuilder.WriteString("Successfully completed all nested planned steps (respond):\n")
		for _, s := range state.Steps {
			summaryBuilder.WriteString(fmt.Sprintf("- %s: %s\n", s.Action, s.Observation))
		}

		return brain.NextStep{
			Action:  brain.ActionFinish,
			Payload: summaryBuilder.String(),
		}, nil

	case brain.EventError:
		return brain.NextStep{
			Action:  brain.ActionFinish,
			Payload: fmt.Sprintf("Execution failed due to error: %v", event.Payload),
		}, nil

	default:
		return brain.NextStep{}, fmt.Errorf("unsupported event type: %s", event.Type)
	}
}
