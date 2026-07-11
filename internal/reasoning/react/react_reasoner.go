package react

import (
	"context"
	"fmt"
	"strings"

	"github.com/qtopie/domour/internal/brain"
)

// CerebrumReActReasoner implements the brain.Reasoner interface.
// It manages a global ReAct loop where the Cerebrum reasons and the Cerebellum executes.
type CerebrumReActReasoner struct{}

func init() {
	brain.RegisterReasoner("react", &CerebrumReActReasoner{})
}

// Name returns the identifier of the reasoner.
func (r *CerebrumReActReasoner) Name() string {
	return "react"
}

// Decide coordinates the event relay for global ReAct (Thought -> Act -> Observe).
func (r *CerebrumReActReasoner) Decide(ctx context.Context, state *brain.State, event brain.Event) (brain.NextStep, error) {
	if state.ReasonerState == nil {
		state.ReasonerState = make(map[string]interface{})
	}

	switch event.Type {
	case brain.EventUserQuery:
		// Step 1: Initial query. Ask Cerebrum to think and choose the first action.
		query := event.Payload.(string)
		state.GlobalGoal = query
		state.ReasonerState["query"] = query
		state.ReasonerState["history_prompt"] = fmt.Sprintf("Solve the goal: %s", query)

		return brain.NextStep{
			Action:  brain.ActionCallLLM,
			Payload: state.ReasonerState["history_prompt"].(string),
		}, nil

	case brain.EventLLMResponse:
		// Step 2: Cerebrum thought step. Inspect the recommended action or final answer.
		respText := event.Payload.(string)
		lines := strings.Split(respText, "\n")
		intent := ""
		if len(lines) > 0 {
			intent = strings.TrimSpace(lines[0])
		}

		// Append this LLM step to history context
		state.ReasonerState["history_prompt"] = state.ReasonerState["history_prompt"].(string) + "\nCerebrum: " + respText

		// If Cerebrum decided to conclude, finish and return output
		if intent == "respond" || strings.Contains(strings.ToLower(respText), "respond") {
			var responseText = respText
			if len(lines) > 1 {
				responseText = strings.Join(lines[1:], "\n")
			}
			return brain.NextStep{
				Action:  brain.ActionFinish,
				Payload: responseText,
			}, nil
		}

		// Extract tool action to execute
		var toolAction = "inspect_env"
		if len(lines) > 1 {
			toolAction = strings.TrimSpace(lines[1])
		}

		// Send task to the Cerebellum for execution
		return brain.NextStep{
			Action:  brain.ActionCallTool,
			Payload: toolAction,
		}, nil

	case brain.EventExecResult:
		// Step 3: Tool observation returned from Cerebellum.
		obsText := event.Payload.(string)

		// ReAct loop guard: increment counter, check against max
		state.ToolCallCount++
		maxTools := state.MaxToolCalls
		if maxTools == 0 {
			maxTools = 20 // default
		}
		if state.ToolCallCount >= maxTools {
			errMsg := fmt.Sprintf("__brain_review__: ReAct loop exceeded %d tool calls in session %s. "+
				"Original goal: %s. Last tool: %s. "+
				"Please review the approach and re-plan if necessary.",
				maxTools, state.SessionID, state.GlobalGoal, obsText)
			return brain.NextStep{
				Action:  brain.ActionFinish,
				Payload: errMsg,
			}, nil
		}

		// Append observation to history and trigger next thinking step in Cerebrum
		history := state.ReasonerState["history_prompt"].(string) + "\nObservation: " + obsText
		state.ReasonerState["history_prompt"] = history

		return brain.NextStep{
			Action:  brain.ActionCallLLM,
			Payload: history,
		}, nil

	case brain.EventError:
		return brain.NextStep{
			Action:  brain.ActionFinish,
			Payload: fmt.Sprintf("ReAct loop failed: %v", event.Payload),
		}, nil

	default:
		return brain.NextStep{}, fmt.Errorf("unsupported event type: %s", event.Type)
	}
}
