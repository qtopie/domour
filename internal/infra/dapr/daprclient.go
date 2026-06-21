package dapr

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// WorkflowState represents the state and result of a workflow execution.
type WorkflowState struct {
	Status string          `json:"status"` // "running", "completed", "failed"
	Result *schema.Message `json:"result"`
	Err    error           `json:"error"`
}

// DurableAgentOrchestrator defines the engine interface for orchestrating durable agent workflows.
type DurableAgentOrchestrator interface {
	StartWorkflow(ctx context.Context, agentID string, input any) (string, error)
	GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowState, error)
}

// AgentWorkflowInput contains all necessary fields to run the ReAct tool calling loop.
type AgentWorkflowInput struct {
	SessionID   string            `json:"session_id"`
	Messages    []*schema.Message `json:"messages"`
	Provider    string            `json:"provider"`
	Model       string            `json:"model"`
	StreamFinal bool              `json:"stream_final"`
	Stage       string            `json:"stage"`
}
