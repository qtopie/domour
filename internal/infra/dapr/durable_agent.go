package dapr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/cloudwego/eino/schema"
	daprClient "github.com/dapr/go-sdk/client"
	"github.com/dapr/go-sdk/workflow"
)

// DurableAgent represents an agent whose execution flow is managed by Dapr Workflows.
// It ensures that execution is fault-tolerant and recovers automatically from interruptions.
type DurableAgent struct {
	name   string
	worker *workflow.WorkflowWorker
}

// NewDurableAgent creates a new DurableAgent.
func NewDurableAgent(name string) (*DurableAgent, error) {
	dc, err := getFreshDaprClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create dapr client: %w", err)
	}

	w, err := workflow.NewWorker(workflow.WorkerWithDaprClient(dc))
	if err != nil {
		dc.Close()
		return nil, fmt.Errorf("failed to create workflow worker: %w", err)
	}

	return &DurableAgent{
		name:   name,
		worker: w,
	}, nil
}

// RegisterStep registers an agent execution step (activity) with the Dapr workflow engine.
func (a *DurableAgent) RegisterStep(step workflow.Activity) error {
	return a.worker.RegisterActivity(step)
}

// RegisterWorkflow registers the core agent reasoning/execution loop (workflow) with the Dapr workflow engine.
func (a *DurableAgent) RegisterWorkflow(wf workflow.Workflow) error {
	return a.worker.RegisterWorkflow(wf)
}

// Start starts the agent's workflow runner worker.
func (a *DurableAgent) Start() error {
	slog.Info("Starting DurableAgent worker", "agent", a.name)
	return a.worker.Start()
}

// Shutdown stops the agent's workflow runner worker.
func (a *DurableAgent) Shutdown() {
	slog.Info("Shutting down DurableAgent worker", "agent", a.name)
	a.worker.Shutdown()
}

// DurableAgentClient manages client interactions with DurableAgents.
type DurableAgentClient struct {
	client *workflow.Client
	dc     daprClient.Client
}

// NewDurableAgentClient creates a new DurableAgentClient.
func NewDurableAgentClient() (*DurableAgentClient, error) {
	dc, err := getFreshDaprClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create dapr client: %w", err)
	}

	c, err := workflow.NewClient(workflow.WithDaprClient(dc))
	if err != nil {
		dc.Close()
		return nil, fmt.Errorf("failed to create dapr workflow client: %w", err)
	}
	return &DurableAgentClient{
		client: c,
		dc:     dc,
	}, nil
}

// Close closes the underlying Dapr client.
func (c *DurableAgentClient) Close() {
	if c.dc != nil {
		c.dc.Close()
	}
}

// StartAgentTask starts a new durable task execution for an agent.
func (c *DurableAgentClient) StartAgentTask(ctx context.Context, workflowName string, instanceID string, input interface{}) (string, error) {
	id, err := c.client.ScheduleNewWorkflow(ctx, workflowName, workflow.WithInstanceID(instanceID), workflow.WithInput(input))
	if err != nil {
		return "", fmt.Errorf("failed to start agent task: %w", err)
	}
	return id, nil
}

// StartWorkflow starts a workflow using Dapr client.
func (c *DurableAgentClient) StartWorkflow(ctx context.Context, agentID string, input any) (string, error) {
	return c.StartAgentTask(ctx, "DurableAgentTaskWorkflow", agentID, input)
}

// GetTaskStatus retrieves the current status of a durable agent task.
func (c *DurableAgentClient) GetTaskStatus(ctx context.Context, instanceID string) (*workflow.Metadata, error) {
	meta, err := c.client.FetchWorkflowMetadata(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent task status: %w", err)
	}
	return meta, nil
}

// GetWorkflowStatus retrieves the workflow status from Dapr and converts it to the common WorkflowState.
func (c *DurableAgentClient) GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowState, error) {
	meta, err := c.GetTaskStatus(ctx, workflowID)
	if err != nil {
		return nil, err
	}

	state := &WorkflowState{
		Status: meta.RuntimeStatus.String(),
	}

	if meta.RuntimeStatus == workflow.StatusCompleted && meta.SerializedOutput != "" {
		var res schema.Message
		if err := json.Unmarshal([]byte(meta.SerializedOutput), &res); err == nil {
			state.Result = &res
		}
	} else if meta.RuntimeStatus == workflow.StatusFailed || meta.RuntimeStatus == workflow.StatusTerminated {
		state.Err = fmt.Errorf("workflow failed with status: %s", meta.RuntimeStatus)
	}

	return state, nil
}

func getFreshDaprClient() (daprClient.Client, error) {
	port := os.Getenv("DAPR_GRPC_PORT")
	if port == "" {
		port = "50001"
	}
	return daprClient.NewClientWithPort(port)
}
