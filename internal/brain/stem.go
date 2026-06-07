package brain

import "context"

const StemLayerName = "stem"

// Signal is the unified signal encapsulation entering the system, which can come from chat/copilot/autopilot, etc.
type Signal struct {
	SessionID   string            `json:"session_id"`
	Entry       string            `json:"entry"`
	Workspace   string            `json:"workspace,omitempty"`
	Message     string            `json:"message,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// Decision is the minimum processing result of the Stem layer for input signals.
type Decision struct {
	Allowed bool              `json:"allowed"`
	Route   string            `json:"route"`
	Reason  string            `json:"reason,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// StemModule defines the minimum responsibility of the Stem layer: receive inbound signals, guard, and perform initial routing.
type StemModule interface {
	Name() string
	Process(ctx context.Context, signal Signal) (Decision, error)
}
