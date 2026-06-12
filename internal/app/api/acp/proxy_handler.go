package acpapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type proxyHandler struct {
	chatModel model.ChatModel
}

func NewProxyHandler(chatModel model.ChatModel) *proxyHandler {
	return &proxyHandler{chatModel: chatModel}
}

func (h *proxyHandler) Handle(ctx context.Context, method string, params []byte) (any, error) {
	switch method {
	case "prompts/get":
		return h.handlePromptGet(ctx, params)
	case "tools/call":
		return nil, fmt.Errorf("tools/call not supported in proxy mode")
	default:
		return nil, fmt.Errorf("method %s not supported in proxy mode", method)
	}
}

func (h *proxyHandler) handlePromptGet(ctx context.Context, params []byte) (any, error) {
	var req struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Extract the prompt from common fields
	var prompt string
	if content, ok := req.Arguments["content"].(string); ok {
		prompt = content
	} else if p, ok := req.Arguments["prompt"].(string); ok {
		prompt = p
	} else if len(req.Arguments) > 0 {
		// Fallback: try to find any string field if content/prompt are missing
		for _, v := range req.Arguments {
			if s, ok := v.(string); ok {
				prompt = s
				break
			}
		}
	}

	if prompt == "" {
		// Last resort fallback
		prompt = fmt.Sprintf("%v", req.Arguments)
	}
	
	msg := schema.UserMessage(prompt)
	
	resp, err := h.chatModel.Generate(ctx, []*schema.Message{msg})
	if err != nil {
		return nil, err
	}

	// MCP prompts/get returns a list of messages
	return map[string]any{
		"messages": []map[string]any{
			{
				"role": "assistant",
				"content": map[string]any{
					"type": "text",
					"text": resp.Content,
				},
			},
		},
	}, nil
}
