package acpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/qtopie/domour/ark/acp"
)

type session struct {
	id          string
	mode        string // "proxy" or "cognitive"
	transport   Transport
	initialized bool
	handler     acp.Handler
	mu          sync.Mutex
}

func NewSession(transport Transport) *session {
	return &session{
		id:        uuid.New().String(),
		mode:      acp.ModeCognitive, // Default mode
		transport: transport,
	}
}

func (s *session) ID() string {
	return s.id
}

func (s *session) Mode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

func (s *session) HandleMessage(ctx context.Context, req *acp.JSONRPCRequest) (*acp.JSONRPCResponse, error) {
	if !s.initialized {
		if req.Method != acp.MethodInitialize {
			return nil, fmt.Errorf("first message must be %s", acp.MethodInitialize)
		}
		return s.handleInitialize(ctx, req)
	}

	if req.Method == acp.MethodInitialized {
		// Just a notification, no response needed.
		return nil, nil
	}

	if s.handler == nil {
		return nil, fmt.Errorf("no handler assigned for mode %s", s.mode)
	}

	result, err := s.handler.Handle(ctx, req.Method, req.Params)
	if err != nil {
		return &acp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &acp.JSONRPCError{
				Code:    -32000,
				Message: err.Error(),
			},
		}, nil
	}

	resultBytes, _ := json.Marshal(result)
	return &acp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultBytes,
	}, nil
}

func (s *session) handleInitialize(ctx context.Context, req *acp.JSONRPCRequest) (*acp.JSONRPCResponse, error) {
	var initReq acp.InitializeRequest
	if err := json.Unmarshal(req.Params, &initReq); err != nil {
		return nil, fmt.Errorf("failed to unmarshal initialize params: %w", err)
	}

	s.mu.Lock()
	// Extract mode from experimental capabilities
	if val, ok := initReq.Capabilities.Experimental[acp.CapabilityDomourMode]; ok {
		if modeStr, ok := val.(string); ok {
			if modeStr == acp.ModeProxy || modeStr == acp.ModeCognitive {
				s.mode = modeStr
			}
		}
	}
	s.initialized = true
	s.mu.Unlock()

	// Prepare response
	result := acp.InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: acp.ServerCapabilities{
			Experimental: map[string]any{
				acp.CapabilityDomourMode: s.mode,
			},
		},
		ServerInfo: acp.ImplementationInfo{
			Name:    "Domour ACP Server",
			Version: "0.1.0",
		},
	}

	resultBytes, _ := json.Marshal(result)
	return &acp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultBytes,
	}, nil
}

func (s *session) SetHandler(h acp.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

func (s *session) Close() error {
	return s.transport.Close()
}
