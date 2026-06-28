# Unified Model Context Protocol (MCP) Design

This document details the design for providing unified MCP tool support in Domour, bridging local environments and distributed Dapr agent networks.

## 1. Background & Motivation
Model Context Protocol (MCP) is an open standard that enables LLM applications (agents) to connect to external data sources and tools. Both local agent runtimes (like Eino-based execution loops) and distributed agent runtimes (like Dapr Agents) need a unified way to register, discover, and invoke MCP tools.

To support this in Domour, we introduce:
1. A unified API abstraction for MCP clients.
2. A **Local Stdio Client** to interact with local MCP servers running as subprocesses.
3. A **Dapr Client** to route MCP requests to remote nodes managed by Dapr.
4. Framework integration mapping MCP tool specs and schemas to Eino schemas, enabling seamless tool calling.

---

## 2. Unified MCP Client Interface

We define the unified interface in a new package `internal/infra/mcp`:

```go
package mcp

import "context"

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
```

---

## 3. Implementations

### A. Local Stdio Client (`StdioClient`)
A local MCP server runs as a subprocess (e.g., executing Node/Python or compiled binaries). 
- Communicates via `stdin/stdout` using JSON-RPC 2.0.
- Reads JSON-RPC packets delimited by newlines (`\n`).

#### Initialization Flow:
1. `exec.Command` starts the configured server process.
2. Pipe stdin/stdout to reader/writer loops.
3. Send `initialize` JSON-RPC request and wait for the response.
4. Send `notifications/initialized` event.

### B. HTTP/SSE Client (`SSEClient`)
A client that connects to an HTTP-based MCP server.
- Uses HTTP POST to send JSON-RPC requests to the server's endpoint.
- Listens for server-sent messages/responses via an SSE (Server-Sent Events) connection.
- Ideal for connecting to external remote web-hosted MCP servers without Dapr overhead.

### C. Dapr Client (`DaprClient`)
In a distributed deployment, MCP servers might be hosted remotely.
- The `DaprClient` proxies the `Client` methods.
- Method calls are serialized and routed to a Dapr Service or Actor hosting the target MCP client.

---

## 4. Configuration Schema

We extend `DomourConfig` in `internal/config/config.go` to support declaring MCP servers:

```json
{
  "mcp_servers": {
    "git": {
      "type": "stdio",
      "command": "node",
      "args": ["/path/to/mcp-server-git/dist/index.js"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "your-token"
      }
    },
    "web-search": {
      "type": "sse",
      "url": "http://localhost:3000/sse"
    },
    "k8s-remote": {
      "type": "dapr",
      "app_id": "remote-mcp-service"
    }
  }
}
```

---

## 5. Tool Manager Integration

The `tool.Manager` in `internal/bionic/tool/` will register configured MCP servers:
1. Iterate over configured `mcp_servers` in `DomourConfig`.
2. For each server:
   - Instantiate the appropriate `mcp.Client` (Stdio or Dapr).
   - Fetch the list of tools via `ListTools`.
   - Register each tool in the `tool.Manager` using `NewMCPTool`.
3. When the LLM requests a tool call, the manager invokes the client's `CallTool` method.

### Mapping Schema to Eino
During intent discovery and planning, MCP tool definitions (JSON Schema) must be transformed into Eino's `schema.ToolInfo` representation.
We will dynamically build Eino tool schemas from the `InputSchema` metadata maps returned by `ListTools`.

---

## 6. Execution & Safety (Veto)
All MCP tools pass through the Brainstem's safety filter (`Veto`) before execution, preserving Domour's safety principles.
