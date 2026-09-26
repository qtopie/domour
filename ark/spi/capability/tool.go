package capability

import (
	"context"
	"encoding/json"
	"io"
)

// ToolSpec describes a tool's identity and input schema.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"` // Standard JSON Schema
}

// ToolInvoker defines the execution contract for tools.
// In-Process Go functions, local CLI sub-processes, and external MCP servers
// all conform to this boundary.
type ToolInvoker interface {
	Spec() ToolSpec
	Invoke(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
	io.Closer
}
