// Package agent provides the unified Agent interface and constructors for Domour SDK.
// It follows Google adk-go design principles by exposing pure interfaces and lightweight
// handles, while hiding execution engines and proxy clients behind unexported fields.
package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// Input represents the input parameters for executing an agent workflow.
type Input struct {
	SessionID            string            `json:"session_id,omitempty"`
	Message              string            `json:"message"`
	SystemPromptOverride string            `json:"system_prompt_override,omitempty"`
	Provider             string            `json:"provider,omitempty"`
	Model                string            `json:"model,omitempty"`
	Config               map[string]any    `json:"config,omitempty"`
	Messages             []*schema.Message `json:"messages,omitempty"`
}

// Output represents the completed result of an agent execution turn.
type Output struct {
	Content  string          `json:"content"`
	Provider string          `json:"provider,omitempty"`
	Model    string          `json:"model,omitempty"`
	Message  *schema.Message `json:"message,omitempty"`
}

// StreamEvent represents a single token chunk or step event during streaming execution.
type StreamEvent struct {
	Type     string `json:"type"` // "text", "thought", "tool_call", "error"
	Content  string `json:"content,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Err      error  `json:"-"`
}

// Agent is the primary unified interface for running AI agents in Domour.
type Agent interface {
	Name() string
	Run(ctx context.Context, input *Input) (*Output, error)
	Stream(ctx context.Context, input *Input, handler func(event *StreamEvent) error) (*Output, error)
}

// Options configures an LLMAgent instance.
type options struct {
	provider     string
	model        string
	systemPrompt string
	baseURL      string
	apiKey       string
}

// Option represents a functional option for configuring an Agent.
type Option func(*options)

// WithProvider sets the LLM provider name (e.g., "gemini", "openai", "deepseek").
func WithProvider(p string) Option {
	return func(o *options) {
		o.provider = p
	}
}

// WithModel sets the model name (e.g., "gemini-2.5-flash", "gpt-4o").
func WithModel(m string) Option {
	return func(o *options) {
		o.model = m
	}
}

// WithSystemPrompt sets the system prompt for the agent.
func WithSystemPrompt(sp string) Option {
	return func(o *options) {
		o.systemPrompt = sp
	}
}

// LLMAgent is a concrete Agent implementation that encapsulates an LLM reasoning engine.
type LLMAgent struct {
	name   string
	opts   options
	engine runnerEngine // Internal unexported interface for execution
}

type runnerEngine interface {
	Execute(ctx context.Context, input *Input) (*Output, error)
	StreamExecute(ctx context.Context, input *Input, handler func(event *StreamEvent) error) (*Output, error)
}

// NewLLMAgent constructs a new LLMAgent instance with functional options.
func NewLLMAgent(name string, opts ...Option) *LLMAgent {
	var cfg options
	for _, opt := range opts {
		opt(&cfg)
	}
	return &LLMAgent{
		name: name,
		opts: cfg,
	}
}

func (a *LLMAgent) Name() string {
	return a.name
}

func (a *LLMAgent) Run(ctx context.Context, input *Input) (*Output, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}
	if a.engine != nil {
		return a.engine.Execute(ctx, input)
	}
	// Default fallback content if engine is not bound
	return &Output{
		Content:  input.Message,
		Provider: a.opts.provider,
		Model:    a.opts.model,
	}, nil
}

func (a *LLMAgent) Stream(ctx context.Context, input *Input, handler func(event *StreamEvent) error) (*Output, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}
	if a.engine != nil {
		return a.engine.StreamExecute(ctx, input, handler)
	}
	if handler != nil {
		_ = handler(&StreamEvent{Type: "text", Content: input.Message, Provider: a.opts.provider, Model: a.opts.model})
	}
	return &Output{
		Content:  input.Message,
		Provider: a.opts.provider,
		Model:    a.opts.model,
	}, nil
}
