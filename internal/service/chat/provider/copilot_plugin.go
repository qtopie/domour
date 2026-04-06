package provider

import (
	"context"
	"fmt"
	"sync"

	cfg "github.com/qtopie/domour/internal/app/config"
	copilotPkg "github.com/qtopie/domour/internal/pkg/copilot"
	"github.com/qtopie/domour/internal/pkg/copilot/shared"
	"github.com/qtopie/domour/internal/pkg/plugin"
)

type CopilotPluginProvider struct {
	pluginManager *plugin.PluginManager
	mu            sync.Mutex
	currentName   string
	currentPlugin copilotPkg.CopilotPlugin
}

func NewCopilotPluginProvider(pluginManager *plugin.PluginManager) *CopilotPluginProvider {
	return &CopilotPluginProvider{pluginManager: pluginManager}
}

func (p *CopilotPluginProvider) Name() string {
	return "copilot-plugin"
}

func (p *CopilotPluginProvider) Generate(ctx context.Context, req GenerateRequest) (<-chan Chunk, error) {
	if err := p.loadAndRefreshPlugin(); err != nil {
		return nil, err
	}

	history := make([]shared.Message, 0, len(req.History))
	for _, h := range req.History {
		history = append(history, shared.Message{Role: h.Role, Content: h.Content})
	}

	stream, err := p.currentPlugin.Chat(shared.UserRequest{
		SessionId: req.SessionID,
		Message:   req.Content,
		History:   history,
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
				out <- Chunk{Done: true}
				return
			case chunk, ok := <-stream:
				if !ok {
					out <- Chunk{Done: true}
					return
				}
				if chunk.Content != "" {
					out <- Chunk{Text: chunk.Content}
				}
				if chunk.IsLast {
					out <- Chunk{Done: true}
					return
				}
			}
		}
	}()

	return out, nil
}

func (p *CopilotPluginProvider) loadAndRefreshPlugin() error {
	pluginName := cfg.GetAppConfig().GetString("plugins.copilot")
	if pluginName == "" {
		return fmt.Errorf("no copilot plugin specified in configuration")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.currentName == pluginName && p.currentPlugin != nil {
		return nil
	}

	if err := p.pluginManager.LoadPlugin("copilot", pluginName); err != nil {
		return err
	}

	rawPlugin, ok := p.pluginManager.GetPlugin("copilot", pluginName)
	if !ok {
		return fmt.Errorf("copilot plugin %s not found", pluginName)
	}

	switch typed := rawPlugin.(type) {
	case copilotPkg.CopilotPlugin:
		p.currentPlugin = typed
	case *copilotPkg.CopilotPlugin:
		p.currentPlugin = *typed
	default:
		return fmt.Errorf("plugin %s does not implement CopilotPlugin", pluginName)
	}

	p.currentName = pluginName
	return nil
}
