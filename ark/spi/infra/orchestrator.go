package infra

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// WorkflowState represents the execution status and output message of a workflow.
type WorkflowState struct {
	Status string          `json:"status"` // "running", "completed", "failed"
	Result *schema.Message `json:"result,omitempty"`
	Err    error           `json:"error,omitempty"`
}

// WorkflowInput defines the neutral execution parameters for an agent workflow run.
type WorkflowInput struct {
	SessionID   string            `json:"session_id"`
	Messages    []*schema.Message `json:"messages"`
	Provider    string            `json:"provider"`
	Model       string            `json:"model"`
	StreamFinal bool              `json:"stream_final"`
	Stage       string            `json:"stage"`
	Reasoning   string            `json:"reasoning,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// AgentOrchestrator defines the engine interface for orchestrating agent workflows.
type AgentOrchestrator interface {
	StartWorkflow(ctx context.Context, workflowID string, input any) (string, error)
	GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowState, error)
	WaitForWorkflow(ctx context.Context, workflowID string) (*WorkflowState, error)
	EventBus() EventBus
}

