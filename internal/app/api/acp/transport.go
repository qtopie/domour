package acpapi

import (
	"context"
	"io"

	"github.com/qtopie/domour/ark/acp"
)

// Transport defines the communication medium (e.g., stdio, SSE).
type Transport interface {
	// ReadMessage reads the next JSON-RPC request from the transport.
	ReadMessage(ctx context.Context) (*acp.JSONRPCRequest, error)
	// WriteMessage writes a JSON-RPC response or notification to the transport.
	WriteMessage(ctx context.Context, msg any) error
	// Closer
	io.Closer
}
