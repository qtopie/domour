package engine

import (
	"context"

	"github.com/qtopie/domour/internal/infra/dapr"
	"github.com/qtopie/domour/internal/infra/eventbus"
)

// WorkflowState represents the state and result of a workflow execution.
type WorkflowState = dapr.WorkflowState

// AgentWorkflowInput contains all necessary fields to run an agent workflow loop.
type AgentWorkflowInput = dapr.AgentWorkflowInput

// AgentOrchestrator defines the engine interface for orchestrating agent workflows.
type AgentOrchestrator interface {
	StartWorkflow(ctx context.Context, workflowID string, input any) (string, error)
	GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowState, error)
	WaitForWorkflow(ctx context.Context, workflowID string) (*WorkflowState, error)
	EventBus() eventbus.EventBus
}
