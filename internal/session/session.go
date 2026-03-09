package session

import (
	"time"

	"github.com/qtopie/domour/pkg/copilot/shared"
)

type Session struct {
	ID        string           `json:"id"`
	UserID    string           `json:"user_id"`
	History   []shared.Message `json:"history"` // Store conversation history
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}
