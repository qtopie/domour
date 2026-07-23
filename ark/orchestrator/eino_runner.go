package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/engine"
	"github.com/qtopie/domour/internal/infra/dapr"
	localorch "github.com/qtopie/domour/internal/infra/dapr/local"
	localbus "github.com/qtopie/domour/internal/infra/eventbus/local"
)

// EinoNativeRunner executes agents natively in-process using the Eino framework graph.
type EinoNativeRunner struct {
	orch *localorch.LocalOrchestrator
}

// NewEinoNativeRunner creates a new in-process Eino native runner.
func NewEinoNativeRunner(eng engine.Engine) *EinoNativeRunner {
	eb := localbus.NewEventBus()
	orch := localorch.NewLocalOrchestrator(eng, eb)
	return &EinoNativeRunner{orch: orch}
}

// Run synchronously executes the agent pipeline using the Eino in-process graph.
func (r *EinoNativeRunner) Run(ctx context.Context, input *AgentInput) (*schema.Message, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}
	wfInput := dapr.AgentWorkflowInput{
		SessionID: input.SessionID,
		Stage:     input.Stage,
		Provider:  input.Provider,
		Model:     input.Model,
		Messages:  input.Messages,
	}

	workflowID := fmt.Sprintf("eino-run-%s", input.SessionID)
	_, err := r.orch.StartWorkflow(ctx, workflowID, wfInput)
	if err != nil {
		return nil, fmt.Errorf("start eino native workflow: %w", err)
	}

	state, err := r.orch.WaitForWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("get workflow status: %w", err)
	}
	if state.Err != nil {
		return nil, state.Err
	}
	return state.Result, nil
}

// Stream streams execution events during the Eino in-process graph run.
func (r *EinoNativeRunner) Stream(ctx context.Context, input *AgentInput, yield func(event *StreamEvent) error) (*schema.Message, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}
	wfInput := dapr.AgentWorkflowInput{
		SessionID: input.SessionID,
		Stage:     input.Stage,
		Provider:  input.Provider,
		Model:     input.Model,
		Messages:  input.Messages,
	}

	workflowID := fmt.Sprintf("eino-stream-%s", input.SessionID)
	eb := r.orch.EventBus()
	topic := fmt.Sprintf("agent/workflow/%s/stream", workflowID)

	doneCh := make(chan struct{})
	if eb != nil {
		sub, err := eb.Subscribe(ctx, topic, func(data []byte) {
			var evt shared.MotorStreamEvent
			if err := json.Unmarshal(data, &evt); err == nil {
				streamEvt := &StreamEvent{
					Type:    fmt.Sprintf("%d", evt.Type),
					Content: evt.Content,
				}
				if evt.Err != nil {
					streamEvt.Error = evt.Err.Error()
				}
				if yield != nil {
					_ = yield(streamEvt)
				}
				if evt.Done {
					close(doneCh)
				}
			}
		})
		if err == nil && sub != nil {
			defer sub.Unsubscribe()
		}
	}

	_, err := r.orch.StartWorkflow(ctx, workflowID, wfInput)
	if err != nil {
		return nil, fmt.Errorf("start eino native workflow: %w", err)
	}

	select {
	case <-doneCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	state, err := r.orch.GetWorkflowStatus(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	if state.Err != nil {
		return nil, state.Err
	}
	return state.Result, nil
}
