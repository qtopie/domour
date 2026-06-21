package memory

import (
	"context"
	"sync"
	"time"
)

type cacheEntry[V any] struct {
	value      V
	expiration time.Time
}

// Cache implements the generic Cache interface in-memory.
type Cache[V any] struct {
	mu    sync.RWMutex
	store map[string]cacheEntry[V]
}

func NewCache[V any]() *Cache[V] {
	return &Cache[V]{
		store: make(map[string]cacheEntry[V]),
	}
}

func (c *Cache[V]) Set(ctx context.Context, key string, value V, ttl time.Duration) error {
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}

	c.mu.Lock()
	c.store[key] = cacheEntry[V]{
		value:      value,
		expiration: exp,
	}
	c.mu.Unlock()

	return nil
}

func (c *Cache[V]) Get(ctx context.Context, key string) (V, bool, error) {
	c.mu.RLock()
	entry, ok := c.store[key]
	c.mu.RUnlock()

	var zero V
	if !ok {
		return zero, false, nil
	}

	if !entry.expiration.IsZero() && time.Now().After(entry.expiration) {
		c.mu.Lock()
		delete(c.store, key)
		c.mu.Unlock()
		return zero, false, nil
	}

	return entry.value, true, nil
}

func (c *Cache[V]) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	delete(c.store, key)
	c.mu.Unlock()
	return nil
}

func (c *Cache[V]) Close() error {
	c.mu.Lock()
	c.store = make(map[string]cacheEntry[V])
	c.mu.Unlock()
	return nil
}
