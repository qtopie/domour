package runtime

import "context"

// CoreTarget identifies whether a task should be processed by the Primary Core or Secondary Core.
type CoreTarget string

const (
	// CorePrimary targets the Primary Core (Cerebrum): complex reasoning, deep multi-step planning.
	CorePrimary CoreTarget = "primary"
	// CoreSecondary targets the Secondary Core (Cerebellum): fast reflexive responses, instant calibration, error correction.
	CoreSecondary CoreTarget = "secondary"
)

// RouteDecision describes the scheduling and reasoning decision made by Router.
type RouteDecision struct {
	TargetCore   CoreTarget        `json:"target_core"`             // "primary" | "secondary"
	FastPath     bool              `json:"fast_path"`               // True if request can bypass heavy planning and execute directly
	Mode         string            `json:"mode"`                    // "simple" | "react" | "deep_think" | "planner"
	RequiredTags []string          `json:"required_tags,omitempty"` // Required model tags (e.g. "reasoning", "pro")
	PreferTags   []string          `json:"prefer_tags,omitempty"`   // Preferred model tags (e.g. "flash", "lite", "local")
	Meta         map[string]string `json:"meta,omitempty"`          // Additional metadata to pass through
}

// Router determines which reasoning engine, execution mode, and model tier to route incoming requests to.
// It realizes the biological dual-pathway model (Low Road / Fast Path vs High Road / Deep Planning).
type Router interface {
	Route(ctx context.Context, sessionID, message string, meta map[string]string) (RouteDecision, error)
}
