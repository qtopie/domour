package acp

import (
	"context"
)

// Server defines the interface for an ACP server.
type Server interface {
	// Start begins listening for connections on the provided transport.
	Start(ctx context.Context) error
	// Stop gracefully shuts down the server.
	Stop() error
}

// Session represents an active ACP connection.
type Session interface {
	// ID returns the session's unique identifier.
	ID() string
	// Mode returns the current mode (proxy or cognitive).
	Mode() string
	// HandleMessage processes a single JSON-RPC message.
	HandleMessage(ctx context.Context, msg *JSONRPCRequest) (*JSONRPCResponse, error)
	// Close terminates the session.
	Close() error
}

// Handler is the interface for processing ACP method calls.
type Handler interface {
	// Handle processes a specific ACP method.
	Handle(ctx context.Context, method string, params []byte) (any, error)
}
