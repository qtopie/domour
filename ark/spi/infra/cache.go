package infra

import (
	"context"
	"time"
)

// Cache provides a generic caching interface for key-value items with expiration TTL.
type Cache[V any] interface {
	Get(ctx context.Context, key string) (V, bool, error)
	Set(ctx context.Context, key string, value V, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
}
