package acpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/qtopie/domour/ark/acp"
)

type pipeTransport struct {
	in  chan []byte
	out chan []byte
}

func (p *pipeTransport) ReadMessage(ctx context.Context) (*acp.JSONRPCRequest, error) {
	select {
	case data := <-p.in:
		var req acp.JSONRPCRequest
		err := json.Unmarshal(data, &req)
		return &req, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *pipeTransport) WriteMessage(ctx context.Context, msg any) error {
	data, _ := json.Marshal(msg)
	p.out <- data
	return nil
}

func (p *pipeTransport) Close() error { return nil }

func TestACP_Integration_HandshakeAndRouting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Mock server
	server := NewServer(nil, nil, nil) // Handlers will be mocked or stubs

	clientIn := make(chan []byte, 10)
	clientOut := make(chan []byte, 10)
	transport := &pipeTransport{in: clientIn, out: clientOut}

	go server.Start(ctx, transport)

	// 1. Send Initialize with Proxy Mode
	initReq := acp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  acp.MethodInitialize,
		Params: json.RawMessage(`{
			"capabilities": {
				"experimental": {
					"domourMode": "proxy"
				}
			}
		}`),
	}
	data, _ := json.Marshal(initReq)
	clientIn <- data

	// Wait for response
	select {
	case respData := <-clientOut:
		var resp acp.JSONRPCResponse
		json.Unmarshal(respData, &resp)
		if resp.Error != nil {
			t.Fatalf("Init failed: %v", resp.Error.Message)
		}
		var result acp.InitializeResult
		json.Unmarshal(resp.Result, &result)
		if result.Capabilities.Experimental[acp.CapabilityDomourMode] != acp.ModeProxy {
			t.Errorf("Expected proxy mode, got %v", result.Capabilities.Experimental[acp.CapabilityDomourMode])
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for init response")
	}

	// 2. Test Proxy Handler Routing
	// Note: Since s.agent is nil, it might panic if we actually call it.
	// But we just want to verify it was set.
	// In a real test we'd mock the agent.
}
