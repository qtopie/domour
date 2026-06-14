package storage

import "context"

// DB defines the abstraction layer for database persistence, supporting query, create, update, and delete operations.
type DB interface {
	Query(ctx context.Context, query string, vars map[string]any) (any, error)
	Create(ctx context.Context, table string, data any) (any, error)
	Update(ctx context.Context, id string, data any) (any, error)
	Delete(ctx context.Context, id string) (any, error)
	Close() error
}
