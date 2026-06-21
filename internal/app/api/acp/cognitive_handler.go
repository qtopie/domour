package acpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qtopie/domour/internal/bionic/tool"
	"github.com/qtopie/domour/internal/brain"
	"github.com/qtopie/domour/internal/engine"
)

type cognitiveHandler struct {
	node     *brain.DiencephalonNode
	eng      engine.Engine
	notifyFn func(ctx context.Context, method string, params any) error
}

type acpStreamEventParams struct {
	SessionID     string                   `json:"sessionId"`
	Type          string                   `json:"type"`
	Content       string                   `json:"content,omitempty"`
	Thinking      *acpThinkingDetail      `json:"thinking,omitempty"`
	Collaboration *acpCollaborationDetail `json:"collaboration,omitempty"`
	ToolCall      *acpToolCallDetail      `json:"tool_call,omitempty"`
}

type acpThinkingDetail struct {
	Engine    string `json:"engine"`
	Stage     string `json:"stage"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
}

type acpCollaborationDetail struct {
	FromNode    string `json:"from_node"`
	ToNode      string `json:"to_node"`
	EventType   string `json:"event_type"`
	Description string `json:"description"`
}

type acpToolCallDetail struct {
	ToolName    string `json:"tool_name"`
	ToolID      string `json:"tool_id"`
	Status      string `json:"status"`
	Arguments   string `json:"arguments,omitempty"`
	Observation string `json:"observation,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
}

func NewCognitiveHandler(node *brain.DiencephalonNode, eng engine.Engine, notifyFn func(ctx context.Context, method string, params any) error) *cognitiveHandler {
	return &cognitiveHandler{
		node:     node,
		eng:      eng,
		notifyFn: notifyFn,
	}
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
	sessionID := fmt.Sprintf("acp-%d", time.Now().UnixNano())

	// Set up local reply listener
	replyCh := make(chan brain.MotorFeedback, 1)
	h.node.RegisterListener(sessionID, replyCh)
	defer h.node.UnregisterListener(sessionID)

	// Set up observer for progress notifications
	if h.eng != nil && h.notifyFn != nil {
		h.eng.AddObserver(sessionID, func(ev engine.SignalEvent) {
			var p acpStreamEventParams
			p.SessionID = ev.SessionID

			switch ev.EventType {
			case "tool_call_start", "tool_call_end":
				p.Type = "tool_call"
				var argsStr, obsStr string
				var duration int64
				status := "started"

				if ev.EventType == "tool_call_end" {
					status = "completed"
					if res, ok := ev.Payload.(tool.Result); ok {
						obsStr = res.Observation
					} else if res, ok := ev.Payload.(*tool.Result); ok && res != nil {
						obsStr = res.Observation
					} else {
						obsStr = fmt.Sprintf("%v", ev.Payload)
					}
				} else {
					if cmd, ok := ev.Payload.(tool.Command); ok {
						argsBytes, _ := json.Marshal(cmd.Input)
						argsStr = string(argsBytes)
					} else if cmd, ok := ev.Payload.(*tool.Command); ok && cmd != nil {
						argsBytes, _ := json.Marshal(cmd.Input)
						argsStr = string(argsBytes)
					}
				}

				toolName := ""
				if cmd, ok := ev.Payload.(tool.Command); ok {
					toolName = cmd.Action
				} else if cmd, ok := ev.Payload.(*tool.Command); ok && cmd != nil {
					toolName = cmd.Action
				}

				p.ToolCall = &acpToolCallDetail{
					ToolName:    toolName,
					ToolID:      ev.SessionID,
					Status:      status,
					Arguments:   argsStr,
					Observation: obsStr,
					DurationMs:  duration,
				}

			case "react_thought":
				p.Type = "thinking"
				p.Thinking = &acpThinkingDetail{
					Engine: "react",
					Stage:  "thought",
				}
				p.Content = ev.Description

			default:
				p.Type = "collaboration"
				p.Collaboration = &acpCollaborationDetail{
					FromNode:    ev.FromNode,
					ToNode:      ev.ToNode,
					EventType:   ev.EventType,
					Description: ev.Description,
				}
			}

			_ = h.notifyFn(ctx, "notifications/domour/stream_event", p)
		})
		defer h.eng.RemoveObserver(sessionID)
	}

	// Create a sensory signal with the explicit SessionID
	sig := brain.SensorySignal{
		Ctx:       ctx,
		SessionID: sessionID,
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

	// Wait for correlated response
	select {
	case resp := <-replyCh:
		// Send final reply chunk as standard text notification before closing
		if h.notifyFn != nil {
			_ = h.notifyFn(ctx, "notifications/domour/stream_event", acpStreamEventParams{
				SessionID: sessionID,
				Type:      "text",
				Content:   resp.Output,
			})
		}

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
	return nil, fmt.Errorf("tools/call not implemented in cognitive mode yet")
}
