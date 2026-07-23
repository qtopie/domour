package orchestrator

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/infra/dapr"
)

// DurableAgentRunner executes agents via Dapr Durable Agent Workflows.
type DurableAgentRunner struct {
	client *dapr.DurableAgentClient
}

// NewDurableAgentRunner creates a new DurableAgentRunner.
func NewDurableAgentRunner(client *dapr.DurableAgentClient) *DurableAgentRunner {
	return &DurableAgentRunner{client: client}
}

// Run synchronously executes an agent workflow via Dapr Durable Workflows.
func (r *DurableAgentRunner) Run(ctx context.Context, input *AgentInput) (*schema.Message, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}
	if r.client == nil {
		return nil, fmt.Errorf("durable agent client is not initialized")
	}

	wfInput := dapr.AgentWorkflowInput{
		SessionID: input.SessionID,
		Stage:     input.Stage,
		Provider:  input.Provider,
		Model:     input.Model,
		Messages:  input.Messages,
	}

	workflowID := fmt.Sprintf("durable-run-%s", input.SessionID)
	instanceID, err := r.client.StartWorkflow(ctx, workflowID, wfInput)
	if err != nil {
		return nil, fmt.Errorf("start durable agent workflow: %w", err)
	}

	state, err := r.client.GetWorkflowStatus(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get durable workflow status: %w", err)
	}
	if state.Err != nil {
		return nil, state.Err
	}
	return state.Result, nil
}

// Stream streams execution progress of a durable agent workflow.
func (r *DurableAgentRunner) Stream(ctx context.Context, input *AgentInput, yield func(event *StreamEvent) error) (*schema.Message, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}
	// Run workflow and stream final/intermediate events
	msg, err := r.Run(ctx, input)
	if err != nil {
		if yield != nil {
			_ = yield(&StreamEvent{Type: "error", Error: err.Error()})
		}
		return nil, err
	}

	if yield != nil && msg != nil {
		_ = yield(&StreamEvent{Type: "message", Message: msg, Content: msg.Content})
	}
	return msg, nil
}
