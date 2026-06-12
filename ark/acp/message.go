package acp

import (
	"encoding/json"
)

// JSONRPCRequest represents a standard JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a standard JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a standard JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// InitializeRequest is the first request sent by the client.
type InitializeRequest struct {
	ProtocolVersion string               `json:"protocolVersion"`
	Capabilities    ClientCapabilities   `json:"capabilities"`
	ClientInfo      ImplementationInfo   `json:"clientInfo"`
}

// ClientCapabilities defines what the client can do.
type ClientCapabilities struct {
	Experimental map[string]any `json:"experimental,omitempty"`
}

// ImplementationInfo identifies the client or server.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the response to the initialize request.
type InitializeResult struct {
	ProtocolVersion string               `json:"protocolVersion"`
	Capabilities    ServerCapabilities   `json:"capabilities"`
	ServerInfo      ImplementationInfo   `json:"serverInfo"`
}

// ServerCapabilities defines what the server can do.
type ServerCapabilities struct {
	Experimental map[string]any `json:"experimental,omitempty"`
}

// Method Names
const (
	MethodInitialize = "initialize"
	MethodInitialized = "notifications/initialized"
)

// Domour Experimental Capabilities
const (
	CapabilityDomourMode = "domourMode"
	ModeProxy            = "proxy"
	ModeCognitive        = "cognitive"
)
