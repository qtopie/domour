package session

import (
	"context"
	"sync"

	"github.com/qtopie/domour/internal/pkg/copilot/shared"
)

// MemoryStore is a minimal in-memory session store used by the MVP server.
type MemoryStore struct {
	mu      sync.RWMutex
	history map[string][]shared.Message
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		history: make(map[string][]shared.Message),
	}
}

func (s *MemoryStore) AppendHistory(_ context.Context, sessionID string, msg shared.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history[sessionID] = append(s.history[sessionID], msg)
	return nil
}

func (s *MemoryStore) GetHistory(_ context.Context, sessionID string) ([]shared.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history := s.history[sessionID]
	result := make([]shared.Message, len(history))
	copy(result, history)
	return result, nil
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history = make(map[string][]shared.Message)
	return nil
}
