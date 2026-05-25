package agent

import (
	"context"
	"log"
	"sync"

	"github.com/qtopie/domour/internal/app/config"
	"github.com/qtopie/domour/internal/pkg/brain/diencephalon"
)

type reloadableBrain struct {
	mu    sync.RWMutex
	inner BrainClient
}

func newReloadableBrain() (BrainClient, error) {
	inner, err := newConfiguredBrainClient()
	if err != nil {
		return nil, err
	}

	b := &reloadableBrain{
		inner: inner,
	}

	config.WatchConfig(func() {
		log.Printf("[Agent] Config change detected, reloading brain client...")
		newInner, err := newConfiguredBrainClient()
		if err != nil {
			log.Printf("[Agent] Failed to reload brain client: %v", err)
			return
		}
		b.mu.Lock()
		b.inner = newInner
		b.mu.Unlock()
		log.Printf("[Agent] Brain client reloaded successfully.")
	})

	return b, nil
}

func (b *reloadableBrain) getInner() BrainClient {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.inner
}

func (b *reloadableBrain) GetClient(ctx context.Context, entry string) (diencephalon.Client, error) {
	return b.getInner().GetClient(ctx, entry)
}

func (b *reloadableBrain) StreamChat(ctx context.Context, req BrainChatRequest) (<-chan BrainStreamEvent, error) {
	return b.getInner().StreamChat(ctx, req)
}

func (b *reloadableBrain) StreamAutopilot(ctx context.Context, req BrainAutopilotRequest) (<-chan BrainStreamEvent, error) {
	return b.getInner().StreamAutopilot(ctx, req)
}

func (b *reloadableBrain) StreamCopilot(ctx context.Context, req BrainCopilotRequest) (<-chan BrainStreamEvent, error) {
	return b.getInner().StreamCopilot(ctx, req)
}

func (b *reloadableBrain) ChatReply(ctx context.Context, req BrainChatRequest) (BrainTextResponse, error) {
	return b.getInner().ChatReply(ctx, req)
}

func (b *reloadableBrain) PlanDiagram(ctx context.Context, req BrainDiagramRequest) (BrainDiagramResponse, error) {
	return b.getInner().PlanDiagram(ctx, req)
}

func (b *reloadableBrain) Copilot(ctx context.Context, req BrainCopilotRequest) (BrainTextResponse, error) {
	return b.getInner().Copilot(ctx, req)
}

func (b *reloadableBrain) Autopilot(ctx context.Context, req BrainAutopilotRequest) (BrainTextResponse, error) {
	return b.getInner().Autopilot(ctx, req)
}
