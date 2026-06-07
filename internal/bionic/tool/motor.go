package tool

import "context"

const MotorLayerName = "motor"

// Command describes the smallest action unit to be executed by the Motor layer.
type Command struct {
	ID      string                 `json:"id"`
	Action  string                 `json:"action"`
	Input   map[string]interface{} `json:"input,omitempty"`
	Meta    map[string]string      `json:"meta,omitempty"`
	Confirm bool                   `json:"confirm,omitempty"`
}

// Result is the standard result after the Motor layer executes.
type Result struct {
	CommandID   string            `json:"command_id"`
	Observation string            `json:"observation"`
	Done        bool              `json:"done"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// MotorModule defines the minimum responsibility of the Motor layer: receive action commands and complete execution feedback.
type MotorModule interface {
	Name() string
	Act(ctx context.Context, command Command) (Result, error)
}
