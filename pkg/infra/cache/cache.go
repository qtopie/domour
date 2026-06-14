package cache

import (
	"context"
	"time"
)

// Cache defines the generic interface for temporary L2 cache storage.
type Cache[V any] interface {
	Get(ctx context.Context, key string) (V, bool, error)
	Set(ctx context.Context, key string, value V, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
}
