package cognitor

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/ark/session"
	"github.com/qtopie/domour/internal/cognitor/proxy"
)

// Config defines connection parameters for Cognitor Reasoning Engine.
type Config struct {
	Provider string `json:"provider"` // e.g. "gemini", "deepseek", "openai", "llamacpp"
	APIKey   string `json:"api_key"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	ProxyURL string `json:"proxy_url,omitempty"`
	Debug    bool   `json:"debug,omitempty"`
}

// Request represents an inference or reasoning request.
type Request struct {
	SessionID            string                   `json:"session_id,omitempty"`
	Workspace            string                   `json:"workspace,omitempty"`
	Message              string                   `json:"message"`
	SystemPromptOverride string                   `json:"system_prompt_override,omitempty"`
	Attachments          []*Attachment            `json:"attachments,omitempty"`
	History              []session.Message        `json:"history,omitempty"`
	MemorySummary        string                   `json:"memory_summary,omitempty"`
	Provider             string                   `json:"provider,omitempty"`
	Model                string                   `json:"model,omitempty"`
}

// Response represents a completed non-streaming inference output.
type Response struct {
	Content  string `json:"content"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// StreamEvent represents a single event chunk during streaming inference.
type StreamEvent struct {
	Type      string `json:"type"` // "text", "thinking", "tool_call", "error"
	Content   string `json:"content"`
	Engine    string `json:"engine,omitempty"`
	Stage     string `json:"stage,omitempty"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	Err       error  `json:"-"`
}

// Client is the Cognitor Reasoning Engine client.
// Idiomatic Go: concrete struct with unexported implementation details.
type Client struct {
	cfg   Config
	proxy *proxy.Client
}

// New creates a new Cognitor Reasoning Engine Client.
func New(ctx context.Context, cfg Config) (*Client, error) {
	pCfg := proxy.Config{
		Provider: cfg.Provider,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
		Model:    cfg.Model,
		ProxyURL: cfg.ProxyURL,
		Debug:    cfg.Debug,
	}

	pClient, err := proxy.New(ctx, pCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cognitor backend: %w", err)
	}

	return &Client{
		cfg:   cfg,
		proxy: pClient,
	}, nil
}

// NewClient is an alias for New to maintain naming convenience.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	return New(ctx, cfg)
}

// Provider returns the configured provider name.
func (c *Client) Provider() string {
	if c.proxy != nil {
		return c.proxy.Provider()
	}
	return c.cfg.Provider
}

// Model returns the configured model name.
func (c *Client) Model() string {
	if c.proxy != nil {
		return c.proxy.Model()
	}
	return c.cfg.Model
}

// Generate performs non-streaming completion / inference.
func (c *Client) Generate(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	messages, err := buildSchemaMessages(req)
	if err != nil {
		return nil, fmt.Errorf("failed to build messages: %w", err)
	}

	resp, err := c.proxy.GenerateText(ctx, messages)
	if err != nil {
		return nil, err
	}

	return &Response{
		Content:  resp.Content,
		Provider: resp.Provider,
		Model:    resp.Model,
	}, nil
}

// Stream performs streaming completion / inference.
func (c *Client) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	messages, err := buildSchemaMessages(req)
	if err != nil {
		return nil, fmt.Errorf("failed to build messages: %w", err)
	}

	if c.proxy == nil || c.proxy.Chat == nil {
		return nil, fmt.Errorf("underlying chat model is not initialized")
	}

	reader, err := c.proxy.Chat.Stream(ctx, messages)
	if err != nil {
		return nil, err
	}

	outCh := make(chan StreamEvent, 16)
	go func() {
		defer close(outCh)
		defer reader.Close()

		for {
			chunk, err := reader.Recv()
			if err != nil {
				if err != io.EOF {
					outCh <- StreamEvent{Type: "error", Err: err}
				}
				return
			}

			outCh <- StreamEvent{
				Type:     "text",
				Content:  chunk.Content,
				Provider: c.Provider(),
				Model:    c.Model(),
			}
		}
	}()

	return outCh, nil
}

func buildSchemaMessages(req *Request) ([]*schema.Message, error) {
	var messages []*schema.Message

	if req.SystemPromptOverride != "" {
		messages = append(messages, schema.SystemMessage(req.SystemPromptOverride))
	}

	for _, h := range req.History {
		switch strings.ToLower(h.Role) {
		case "user":
			messages = append(messages, schema.UserMessage(h.Content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(h.Content, nil))
		case "system":
			messages = append(messages, schema.SystemMessage(h.Content))
		}
	}

	if len(req.Attachments) > 0 {
		msg, err := BuildMultimodalMessage(req.Message, req.Attachments)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	} else if req.Message != "" {
		messages = append(messages, schema.UserMessage(req.Message))
	}

	return messages, nil
}
