package runtime

import "context"

// TurnContext provides metadata and prompt state for the current interaction turn.
type TurnContext struct {
	SessionID string            `json:"session_id"`
	UserQuery string            `json:"user_query"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// PreTurnHook intercepts execution before the main reasoning loop runs.
// Returning an error aborts and vetos the current turn.
type PreTurnHook interface {
	Name() string
	Priority() int
	BeforeTurn(ctx context.Context, tc *TurnContext) error
}

// PostTurnHook runs after a reasoning turn completes (e.g. memory consolidation, auditing).
type PostTurnHook interface {
	Name() string
	Priority() int
	AfterTurn(ctx context.Context, tc *TurnContext, finalReply string) error
}
