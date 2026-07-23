package storage

import (
	"context"
	"sync"
	"time"

	"github.com/qtopie/domour/ark/session"
)

// MemoryStore is an in-memory session store.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]session.Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]session.Session),
	}
}

func (s *MemoryStore) AppendHistory(_ context.Context, sessionID string, msg session.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		sess = session.Session{
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

func (s *MemoryStore) GetHistory(_ context.Context, sessionID string) ([]session.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return []session.Message{}, nil
	}
	result := make([]session.Message, len(sess.History))
	copy(result, sess.History)
	return result, nil
}

func (s *MemoryStore) GetSession(_ context.Context, sessionID string) (session.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return session.Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}
	return sess, nil
}

func (s *MemoryStore) SaveSession(_ context.Context, sess session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess.UpdatedAt = time.Now()
	s.sessions[sess.ID] = sess
	return nil
}

func (s *MemoryStore) ListSessions(_ context.Context) ([]session.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]session.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		list = append(list, sess)
	}
	return list, nil
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions = make(map[string]session.Session)
	return nil
}
