package acpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/qtopie/domour/ark/acp"
)

type mockTransport struct {
	lastWritten any
}

func (m *mockTransport) ReadMessage(ctx context.Context) (*acp.JSONRPCRequest, error) { return nil, nil }
func (m *mockTransport) WriteMessage(ctx context.Context, msg any) error            { m.lastWritten = msg; return nil }
func (m *mockTransport) Close() error                                              { return nil }

func TestSession_HandleInitialize(t *testing.T) {
	ctx := context.Background()
	transport := &mockTransport{}
	sess := NewSession(transport)

	// Case 1: Proxy Mode
	initParams := acp.InitializeRequest{
		Capabilities: acp.ClientCapabilities{
			Experimental: map[string]any{
				acp.CapabilityDomourMode: acp.ModeProxy,
			},
		},
	}
	paramsBytes, _ := json.Marshal(initParams)
	req := &acp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  acp.MethodInitialize,
		Params:  paramsBytes,
	}

	resp, err := sess.HandleMessage(ctx, req)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if sess.Mode() != acp.ModeProxy {
		t.Errorf("Expected mode %s, got %s", acp.ModeProxy, sess.Mode())
	}

	var initResult acp.InitializeResult
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if initResult.Capabilities.Experimental[acp.CapabilityDomourMode] != acp.ModeProxy {
		t.Errorf("Expected capability mode %s, got %v", acp.ModeProxy, initResult.Capabilities.Experimental[acp.CapabilityDomourMode])
	}
}

func TestSession_HandleInitialize_Default(t *testing.T) {
	ctx := context.Background()
	transport := &mockTransport{}
	sess := NewSession(transport)

	// Case 2: Default (Proxy) Mode
	initParams := acp.InitializeRequest{
		Capabilities: acp.ClientCapabilities{},
	}
	paramsBytes, _ := json.Marshal(initParams)
	req := &acp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  acp.MethodInitialize,
		Params:  paramsBytes,
	}

	_, err := sess.HandleMessage(ctx, req)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if sess.Mode() != acp.ModeProxy {
		t.Errorf("Expected default mode %s, got %s", acp.ModeProxy, sess.Mode())
	}
}
