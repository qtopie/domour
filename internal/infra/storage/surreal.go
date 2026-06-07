package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/surrealdb/surrealdb.go"
)

type Config struct {
	Address     string
	User        string
	Pass        string
	Namespace   string
	Database    string
	DaprAddress string // Optional: Dapr sidecar HTTP address (e.g. 127.0.0.1:3500)
}

type SurrealDB struct {
	db *surrealdb.DB
}

func NewSurrealDB(cfg Config) (*SurrealDB, error) {
	ctx := context.Background()
	address := cfg.Address

	// If Dapr is enabled, attempt to resolve the address dynamically
	if cfg.DaprAddress != "" {
		resolved, err := resolveEndpointViaDapr(ctx, cfg.DaprAddress)
		if err == nil && resolved != "" {
			fmt.Printf("[SurrealDB] Discovered active endpoint via Dapr: %s\n", resolved)
			address = resolved
		} else if err != nil {
			fmt.Printf("[SurrealDB] Warning: Dapr discovery failed: %v. Falling back to default address %s\n", err, address)
		}
	}

	if address == "" {
		address = "ws://127.0.0.1:8000/rpc"
	}

	db, err := surrealdb.FromEndpointURLString(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to surrealdb at %s: %w", address, err)
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

func resolveEndpointViaDapr(ctx context.Context, daprAddr string) (string, error) {
	// Use Dapr State Store API: GET http://localhost:<daprPort>/v1.0/state/<storeName>/<key>
	url := fmt.Sprintf("http://%s/v1.0/state/db-topology/active-surreal-endpoint", daprAddr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("key 'active-surreal-endpoint' not found in dapr state store")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dapr returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Dapr State Store returns the raw value (or JSON if it was an object)
	// If we saved it as a string, it might be quoted JSON or raw.
	// We'll try to unmarshal as string first.
	var endpoint string
	if err := json.Unmarshal(body, &endpoint); err != nil {
		// If not a JSON string, try raw string
		endpoint = string(body)
	}

	return strings.Trim(endpoint, "\""), nil
}
