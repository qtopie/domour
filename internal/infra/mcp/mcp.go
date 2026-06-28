package mcp

import (
	"context"
	"fmt"
)

// ToolDefinition represents a schema-defined tool exposed by an MCP server.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// CallToolResult represents the execution result of an MCP tool.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock represents a portion of the response content (usually text).
type ContentBlock struct {
	Type string `json:"type"` // e.g., "text", "image"
	Text string `json:"text,omitempty"`
}

// Client is the unified abstraction for interacting with an MCP server.
type Client interface {
	// Initialize performs the handshake with the MCP server.
	Initialize(ctx context.Context) error

	// ListTools lists all available tools from the server.
	ListTools(ctx context.Context) ([]ToolDefinition, error)

	// CallTool invokes a specific tool on the server with arguments.
	CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error)

	// Close terminates the client session and releases resources.
	Close() error
}

// JSON-RPC 2.0 Message structures

type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"` // Int or String or Nil
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Initialize parameters and result

type InitializeParams struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ImplementationInfo `json:"clientInfo"`
}

type ClientCapabilities struct {
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

type ImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ImplementationInfo `json:"serverInfo"`
}

type ServerCapabilities struct {
	Tools        map[string]interface{} `json:"tools,omitempty"`
	Resources    map[string]interface{} `json:"resources,omitempty"`
	Prompts      map[string]interface{} `json:"prompts,omitempty"`
	Logging      map[string]interface{} `json:"logging,omitempty"`
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

type ListToolsResult struct {
	Tools []ToolDefinition `json:"tools"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// NormalizeID converts a JSON-RPC ID to a normalized string key for map lookups.
func NormalizeID(id interface{}) string {
	if id == nil {
		return ""
	}
	switch v := id.(type) {
	case int:
		return fmt.Sprintf("%d", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}
