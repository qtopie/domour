package storage

import (
	"context"
	"time"
)

// Message represents a single message in the conversation history.
type Message struct {
	Role     string `json:"role"` // "user" or "assistant"
	Content  string `json:"content"`
	Time     int64  `json:"time"`
	Seq      int32  `json:"seq,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// ProviderStat tracks per-provider usage metrics for a session.
type ProviderStat struct {
	TokenUsed      int64 `json:"token_used"`
	CallCount      int   `json:"call_count"`
	QuotaExhausted bool  `json:"quota_exhausted"`
}

// Session represents a conversation session, including its history and metadata.
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

// SessionStore handles session persistence and conversation history lookup.
type SessionStore interface {
	GetSession(ctx context.Context, sessionID string) (Session, error)
	SaveSession(ctx context.Context, sess Session) error
	AppendHistory(ctx context.Context, sessionID string, msg Message) error
	GetHistory(ctx context.Context, sessionID string) ([]Message, error)
	ListSessions(ctx context.Context) ([]Session, error)
	Close() error
}

// SessionLocker provides a lock interface for session-based request serialization.
type SessionLocker interface {
	Lock(ctx context.Context, sessionID string) (unlock func(), err error)
}
