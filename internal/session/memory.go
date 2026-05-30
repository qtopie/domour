package session

import (
	"context"
	"sync"
	"time"

	"github.com/qtopie/domour/internal/pkg/copilot/shared"
)

// MemoryStore is an in-memory session store used by the MVP server.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]Session),
	}
}

func (s *MemoryStore) AppendHistory(_ context.Context, sessionID string, msg shared.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		sess = Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
		}
	}

	// Auto-assign sequence number if not set
	if msg.Seq == 0 {
		var maxSeq int32 = 0
		for _, m := range sess.History {
			if m.Seq > maxSeq {
				maxSeq = m.Seq
			}
		}
		if sess.CompressedSeqMax > maxSeq {
			maxSeq = sess.CompressedSeqMax
		}
		msg.Seq = maxSeq + 1
	}

	sess.History = append(sess.History, msg)
	sess.UpdatedAt = time.Now()
	s.sessions[sessionID] = sess
	return nil
}

func (s *MemoryStore) GetHistory(_ context.Context, sessionID string) ([]shared.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return []shared.Message{}, nil
	}
	result := make([]shared.Message, len(sess.History))
	copy(result, sess.History)
	return result, nil
}

func (s *MemoryStore) GetSession(_ context.Context, sessionID string) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}
	return sess, nil
}

func (s *MemoryStore) SaveSession(_ context.Context, sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess.UpdatedAt = time.Now()
	s.sessions[sess.ID] = sess
	return nil
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions = make(map[string]Session)
	return nil
}
