package session

import (
	"time"

	"github.com/qtopie/domour/internal/agent/shared"
)

type Session struct {
	ID                string                  `json:"id"`
	UserID            string                  `json:"user_id"`
	ActiveProvider    string                  `json:"active_provider,omitempty"`
	ActiveModel       string                  `json:"active_model,omitempty"`
	MemorySummary     string                  `json:"memory_summary,omitempty"`
	CompressedSeqMax  int32                   `json:"compressed_seq_max,omitempty"`
	History           []shared.Message        `json:"history"` // Store active conversation history
	ProviderStats     map[string]*ProviderStat `json:"provider_stats,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
}

type ProviderStat struct {
	TokenUsed      int64 `json:"token_used"`
	CallCount      int   `json:"call_count"`
	QuotaExhausted bool  `json:"quota_exhausted"`
}
