package session

import (
	"context"

	"github.com/qtopie/homa/internal/assistant/plugins/copilot/shared"
)

type Store interface {
	AppendHistory(ctx context.Context, sessionID string, msg shared.Message) error
	GetHistory(ctx context.Context, sessionID string) ([]shared.Message, error)
	Close() error
}
