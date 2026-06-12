package acpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qtopie/domour/internal/brain"
)

type cognitiveHandler struct {
	node *brain.DiencephalonNode
}

func NewCognitiveHandler(node *brain.DiencephalonNode) *cognitiveHandler {
	return &cognitiveHandler{node: node}
}

func (h *cognitiveHandler) Handle(ctx context.Context, method string, params []byte) (any, error) {
	switch method {
	case "prompts/get":
		return h.handlePromptGet(ctx, params)
	case "tools/call":
		return h.handleToolCall(ctx, params)
	default:
		return nil, fmt.Errorf("method %s not supported in cognitive mode", method)
	}
}

func (h *cognitiveHandler) handlePromptGet(ctx context.Context, params []byte) (any, error) {
	var req struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	query := fmt.Sprintf("%v", req.Arguments)
	
	// Create a sensory signal
	sig := brain.SensorySignal{
		Ctx:       ctx,
		Source:    "acp-cognitive",
		Data:      query,
		Timestamp: time.Now(),
	}

	// Send to RawSensoryIn
	select {
	case h.node.RawSensoryIn <- sig:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Wait for response on ResponseOut
	// This is a bit tricky because ResponseOut is shared.
	// We need to match by something. In DiencephalonNode.drive, 
	// it uses the sessionID (timestamp based) as ActionID.
	
	// Since we don't have the exact sessionID here (it's generated inside startRoutingLoop),
	// a better way would be to have DiencephalonNode return the ID or allow passing one.
	
	// For MVP, we'll wait for ANY response, but this is NOT thread-safe for multiple concurrent requests.
	// TODO: Refactor brain to support request-response correlation IDs.
	
	select {
	case resp := <-h.node.ResponseOut:
		return map[string]any{
			"messages": []map[string]any{
				{
					"role": "assistant",
					"content": map[string]any{
						"type": "text",
						"text": resp.Output,
					},
				},
			},
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("cognitive processing timed out")
	}
}

func (h *cognitiveHandler) handleToolCall(ctx context.Context, params []byte) (any, error) {
	// Cognitive mode could also expose Domour's tools as ACP tools.
	// For now, let's keep it simple and just handle prompts.
	return nil, fmt.Errorf("tools/call not implemented in cognitive mode yet")
}
