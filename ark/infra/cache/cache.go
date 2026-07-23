package cache

import (
	"fmt"
	"time"

	"github.com/maypok86/otter"
)

// Cache provides an in-memory TTL-based cache.
type Cache[K comparable, V any] struct {
	cache otter.Cache[K, V]
}

// New creates a new Cache instance with the specified capacity and TTL.
func New[K comparable, V any](capacity int, ttl time.Duration) (*Cache[K, V], error) {
	builder, err := otter.NewBuilder[K, V](capacity)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache builder: %w", err)
	}

	c, err := builder.
		WithTTL(ttl).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build cache: %w", err)
	}

	return &Cache[K, V]{cache: c}, nil
}

// NewCache is an alias for New to maintain compatibility.
func NewCache[K comparable, V any](capacity int, ttl time.Duration) (*Cache[K, V], error) {
	return New[K, V](capacity, ttl)
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
	return c.cache.Get(key)
}

func (c *Cache[K, V]) Set(key K, value V) {
	c.cache.Set(key, value)
}

func (c *Cache[K, V]) Delete(key K) {
	c.cache.Delete(key)
}

func (c *Cache[K, V]) Clear() {
	c.cache.Clear()
}
