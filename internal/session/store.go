package session

import (
	"context"

	"github.com/qtopie/domour/internal/pkg/copilot/shared"
)

type Store interface {
	AppendHistory(ctx context.Context, sessionID string, msg shared.Message) error
	GetHistory(ctx context.Context, sessionID string) ([]shared.Message, error)
	GetSession(ctx context.Context, sessionID string) (Session, error)
	SaveSession(ctx context.Context, sess Session) error
	Close() error
}
