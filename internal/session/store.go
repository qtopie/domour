package session

import (
	"context"

	"github.com/qtopie/domour/pkg/copilot/shared"
)

type Store interface {
	AppendHistory(ctx context.Context, sessionID string, msg shared.Message) error
	GetHistory(ctx context.Context, sessionID string) ([]shared.Message, error)
	Close() error
}
