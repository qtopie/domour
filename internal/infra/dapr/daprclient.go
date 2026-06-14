package dapr

import "context"

// DurableAgentOrchestrator defines the engine interface for orchestrating durable agent workflows.
type DurableAgentOrchestrator interface {
	StartWorkflow(ctx context.Context, agentID string, input any) (string, error)
	GetWorkflowStatus(ctx context.Context, workflowID string) (any, error)
}
