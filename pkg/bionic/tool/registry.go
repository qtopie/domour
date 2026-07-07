package tool

import (
	"context"
	"sync"
)

// Tool represents a custom tool definition that can be registered by other modules.
type Tool struct {
	Name        string
	Description string
	Parameters  string // Optional JSON parameters schema (raw JSON string)
	Act         func(ctx context.Context, input map[string]interface{}) (string, error)
}

var (
	mu    sync.Mutex
	tools []Tool
)

// Register registers a public tool.
func Register(t Tool) {
	mu.Lock()
	defer mu.Unlock()
	tools = append(tools, t)
}

// List returns all registered public tools.
func List() []Tool {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Tool, len(tools))
	copy(out, tools)
	return out
}
