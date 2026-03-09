package db

import (
	"context"
	"fmt"

	"github.com/surrealdb/surrealdb.go"
)

type Config struct {
	Address   string
	User      string
	Pass      string
	Namespace string
	Database  string
}

type SurrealDB struct {
	db *surrealdb.DB
}

func NewSurrealDB(cfg Config) (*SurrealDB, error) {
	ctx := context.Background()
	db, err := surrealdb.FromEndpointURLString(ctx, cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to surrealdb: %w", err)
	}

	if _, err := db.SignIn(ctx, map[string]interface{}{
		"user": cfg.User,
		"pass": cfg.Pass,
	}); err != nil {
		return nil, fmt.Errorf("failed to signin to surrealdb: %w", err)
	}

	if err := db.Use(ctx, cfg.Namespace, cfg.Database); err != nil {
		return nil, fmt.Errorf("failed to use namespace/database: %w", err)
	}

	return &SurrealDB{db: db}, nil
}

func (s *SurrealDB) Close() {
	_ = s.db.Close(context.Background())
}

// Query executes a raw SQL query
func (s *SurrealDB) Query(ctx context.Context, sql string, vars map[string]any) (*[]surrealdb.QueryResult[any], error) {
	return surrealdb.Query[any](ctx, s.db, sql, vars)
}

// Create creates a record
func (s *SurrealDB) Create(ctx context.Context, thing string, data interface{}) (interface{}, error) {
	res, err := surrealdb.Create[any](ctx, s.db, thing, data)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return *res, nil
}

// Update updates a record
func (s *SurrealDB) Update(ctx context.Context, thing string, data interface{}) (interface{}, error) {
	res, err := surrealdb.Update[any](ctx, s.db, thing, data)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return *res, nil
}

// Delete deletes a record
func (s *SurrealDB) Delete(ctx context.Context, thing string) (interface{}, error) {
	res, err := surrealdb.Delete[any](ctx, s.db, thing)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return *res, nil
}

// Select selects all records from a table
func (s *SurrealDB) Select(ctx context.Context, thing string) (interface{}, error) {
	res, err := surrealdb.Select[any](ctx, s.db, thing)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return *res, nil
}
