package db

import (
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
	db, err := surrealdb.New(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to surrealdb: %w", err)
	}

	if _, err := db.Signin(map[string]interface{}{
		"user": cfg.User,
		"pass": cfg.Pass,
	}); err != nil {
		return nil, fmt.Errorf("failed to signin to surrealdb: %w", err)
	}

	if _, err := db.Use(cfg.Namespace, cfg.Database); err != nil {
		return nil, fmt.Errorf("failed to use namespace/database: %w", err)
	}

	return &SurrealDB{db: db}, nil
}

func (s *SurrealDB) Close() {
	s.db.Close()
}

// Query executes a raw SQL query
func (s *SurrealDB) Query(sql string, vars interface{}) (interface{}, error) {
	return s.db.Query(sql, vars)
}

// Create creates a record
func (s *SurrealDB) Create(thing string, data interface{}) (interface{}, error) {
	return s.db.Create(thing, data)
}

// Update updates a record
func (s *SurrealDB) Update(thing string, data interface{}) (interface{}, error) {
	return s.db.Update(thing, data)
}

// Delete deletes a record
func (s *SurrealDB) Delete(thing string) (interface{}, error) {
	return s.db.Delete(thing)
}

// Select selects all records from a table
func (s *SurrealDB) Select(thing string) (interface{}, error) {
	return s.db.Select(thing)
}
