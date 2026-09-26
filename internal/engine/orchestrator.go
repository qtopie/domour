package engine

import (
	infraspi "github.com/qtopie/domour/ark/spi/infra"
)

// WorkflowState represents the state and result of a workflow execution.
type WorkflowState = infraspi.WorkflowState

// AgentWorkflowInput contains all necessary fields to run an agent workflow loop.
type AgentWorkflowInput = infraspi.WorkflowInput

// AgentOrchestrator defines the engine interface for orchestrating agent workflows.
type AgentOrchestrator = infraspi.AgentOrchestrator
