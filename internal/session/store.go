package session

import (
	"context"

	"github.com/qtopie/domour/internal/agent/shared"
)

type Store interface {
	AppendHistory(ctx context.Context, sessionID string, msg shared.Message) error
	GetHistory(ctx context.Context, sessionID string) ([]shared.Message, error)
	GetSession(ctx context.Context, sessionID string) (Session, error)
	SaveSession(ctx context.Context, sess Session) error
	ListSessions(ctx context.Context) ([]Session, error)
	Close() error
}
