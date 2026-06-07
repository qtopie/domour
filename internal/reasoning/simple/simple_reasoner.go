package simple

import (
	"context"
	"fmt"
	"strings"

	"github.com/qtopie/domour/internal/brain"
)

// CerebrumSimpleReasoner implements the brain.Reasoner interface.
// It manages a global Simple mode where the Cerebrum reasons once, Diencephalon forwards directly,
// and the Cerebellum verifies and executes.
type CerebrumSimpleReasoner struct{}

func init() {
	brain.RegisterReasoner("simple", &CerebrumSimpleReasoner{})
}

// Name returns the identifier of the reasoner.
func (r *CerebrumSimpleReasoner) Name() string {
	return "simple"
}

// Decide coordinates the event routing for global Simple mode.
func (r *CerebrumSimpleReasoner) Decide(ctx context.Context, state *brain.State, event brain.Event) (brain.NextStep, error) {
	if state.ReasonerState == nil {
		state.ReasonerState = make(map[string]interface{})
	}

	switch event.Type {
	case brain.EventUserQuery:
		// Step 1: Forward user query directly to Cerebrum for cognitive reasoning
		query := event.Payload.(string)
		state.GlobalGoal = query
		state.ReasonerState["query"] = query

		return brain.NextStep{
			Action:  brain.ActionCallLLM,
			Payload: query,
		}, nil

	case brain.EventLLMResponse:
		// Step 2: Cerebrum has responded. Relay to Cerebellum with a verify prefix.
		respText := event.Payload.(string)
		lines := strings.Split(respText, "\n")
		
		// Remove first line if it's metadata/intent (like "execute_workflow")
		content := respText
		if len(lines) > 0 {
			first := strings.TrimSpace(lines[0])
			if first == "execute_workflow" || first == "respond" {
				if len(lines) > 1 {
					content = strings.Join(lines[1:], "\n")
				}
			}
		}
		
		state.ReasonerState["cerebrum_response"] = content

		// Send to Cerebellum for verification/execution
		return brain.NextStep{
			Action:  brain.ActionCallTool,
			Payload: fmt.Sprintf("verify:%s", content),
		}, nil

	case brain.EventExecResult:
		// Step 3: Result received from Cerebellum.
		resText := event.Payload.(string)

		if strings.HasPrefix(resText, "correction:") {
			// There was a problem! Send correction feedback back to Cerebrum
			return brain.NextStep{
				Action:  brain.ActionCallLLM,
				Payload: resText,
			}, nil
		}

		// No problem, finish and return output
		return brain.NextStep{
			Action:  brain.ActionFinish,
			Payload: resText,
		}, nil

	case brain.EventError:
		return brain.NextStep{
			Action:  brain.ActionFinish,
			Payload: fmt.Sprintf("Simple mode failed: %v", event.Payload),
		}, nil

	default:
		return brain.NextStep{}, fmt.Errorf("unsupported event type: %s", event.Type)
	}
}
