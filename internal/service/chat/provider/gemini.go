package provider

import (
	"context"
	"fmt"
	"sync"

	cfg "github.com/qtopie/domour/internal/app/config"
	"github.com/qtopie/domour/internal/pkg/agent"
	"github.com/qtopie/domour/internal/pkg/copilot/shared"
)

type GeminiProvider struct {
	mu     sync.Mutex
	agents map[string]*agent.Agent
}

func NewGeminiProvider() *GeminiProvider {
	return &GeminiProvider{
		agents: make(map[string]*agent.Agent),
	}
}

func (p *GeminiProvider) Name() string {
	return "gemini"
}

func (p *GeminiProvider) Generate(ctx context.Context, req GenerateRequest) (<-chan Chunk, error) {
	appCfg := cfg.GetAppConfig()
	apiKey := appCfg.GetString("gemini.api_key")
	if apiKey == "" {
		return nil, fmt.Errorf("gemini api key not configured")
	}
	model := appCfg.GetString("gemini.model")
	if model == "" {
		model = "gemini-1.5-flash"
	}

	p.mu.Lock()
	a, ok := p.agents[apiKey]
	if !ok {
		var err error
		a, err = agent.NewAgent(ctx, apiKey, model)
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		p.agents[apiKey] = a
	}
	p.mu.Unlock()

	stream, err := a.Run(ctx, shared.UserRequest{
		SessionId: req.SessionID,
		Message:   req.Content,
		History: func() []shared.Message {
			h := make([]shared.Message, 0, len(req.History))
			for _, m := range req.History {
				h = append(h, shared.Message{Role: m.Role, Content: m.Content})
			}
			return h
		}(),
	})
	if err != nil {
		return nil, err
	}

	out := make(chan Chunk)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case text, ok := <-stream:
				if !ok {
					out <- Chunk{Done: true}
					return
				}
				out <- Chunk{Text: text}
			}
		}
	}()

	return out, nil
}
