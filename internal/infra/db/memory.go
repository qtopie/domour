package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/qtopie/domour/ark/session"
)

// MemoryStore implements session.SessionStore in-memory.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]session.Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]session.Session),
	}
}

func (s *MemoryStore) GetSession(ctx context.Context, sessionID string) (session.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return session.Session{ID: sessionID, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
	}
	return sess, nil
}

func (s *MemoryStore) SaveSession(ctx context.Context, sess session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess.ID == "" {
		return fmt.Errorf("session ID cannot be empty")
	}
	sess.UpdatedAt = time.Now()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = sess.UpdatedAt
	}
	s.sessions[sess.ID] = sess
	return nil
}

func (s *MemoryStore) AppendHistory(ctx context.Context, sessionID string, msg session.Message) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	sess.History = append(sess.History, msg)
	return s.SaveSession(ctx, sess)
}

func (s *MemoryStore) GetHistory(ctx context.Context, sessionID string) ([]session.Message, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return sess.History, nil
}

func (s *MemoryStore) ListSessions(ctx context.Context) ([]session.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]session.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out, nil
}

func (s *MemoryStore) Close() error {
	return nil
}
