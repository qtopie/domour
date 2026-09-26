package infra

import (
	"context"
)

// StateStore defines generic key-value storage for persistence or state checkpointing.
type StateStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte) error
	Delete(ctx context.Context, key string) error
	Close() error
}
