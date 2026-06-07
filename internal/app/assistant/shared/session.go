package shared

import "time"

// Session represents a conversation session, including its history and metadata.
// It is defined here (in shared) so that both the bionic/session and infra/storage
// packages can reference it without creating an import cycle.
type Session struct {
	ID               string                    `json:"id"`
	UserID           string                    `json:"user_id"`
	ActiveProvider   string                    `json:"active_provider,omitempty"`
	ActiveModel      string                    `json:"active_model,omitempty"`
	MemorySummary    string                    `json:"memory_summary,omitempty"`
	CompressedSeqMax int32                     `json:"compressed_seq_max,omitempty"`
	History          []Message                 `json:"history"` // Active conversation history
	ProviderStats    map[string]*ProviderStat  `json:"provider_stats,omitempty"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
}

// ProviderStat tracks per-provider usage metrics for a session.
type ProviderStat struct {
	TokenUsed      int64 `json:"token_used"`
	CallCount      int   `json:"call_count"`
	QuotaExhausted bool  `json:"quota_exhausted"`
}
