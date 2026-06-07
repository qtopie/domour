package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/qtopie/domour/internal/infra/cache/l1"
	"github.com/qtopie/domour/internal/infra/cache/l2"
	"github.com/qtopie/domour/internal/infra/storage"
	"github.com/qtopie/domour/internal/infra/bus"
	"github.com/qtopie/domour/internal/bionic/session"
	"github.com/qtopie/domour/internal/app/assistant/shared"
)

type Manager struct {
	l1       *l1.Cache[string, session.Session]
	l2       *l2.Cache[session.Session]
	db       *storage.SurrealDB
	eventBus *bus.EventBus
}

func NewManager(l1Cache *l1.Cache[string, session.Session], l2Cache *l2.Cache[session.Session], database *storage.SurrealDB, eb *bus.EventBus) *Manager {
	return &Manager{
		l1:       l1Cache,
		l2:       l2Cache,
		db:       database,
		eventBus: eb,
	}
}

// GetHistory retrieves the conversation history for a session
func (m *Manager) GetHistory(ctx context.Context, sessionID string) ([]shared.Message, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		// If session not found, return empty history (new session)
		return []shared.Message{}, nil
	}
	return sess.History, nil
}

// AppendHistory appends a message to the session history
func (m *Manager) AppendHistory(ctx context.Context, sessionID string, msg shared.Message) error {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		// Create new session if not exists
		sess = session.Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			History:   []shared.Message{},
		}
	}

	sess.History = append(sess.History, msg)
	sess.UpdatedAt = time.Now()

	return m.Set(ctx, sess)
}

func (m *Manager) Close() error {
	m.l1.Clear()
	// L2 and DB handles are closed by main
	return nil
}

func (m *Manager) Get(ctx context.Context, id string) (session.Session, error) {
	// 1. Check L1
	if s, ok := m.l1.Get(id); ok {
		return s, nil
	}

	// 2. Check L2
	if s, ok, err := m.l2.Get(id); err == nil && ok {
		m.l1.Set(id, s)
		return s, nil
	} else if err != nil {
		log.Printf("L2 cache error: %v", err)
	}

	// 3. Check DB
	// SurrealDB query
	res, err := m.db.Select(ctx, id) // Assuming id is the record ID like "session:123"
	if err != nil {
		return session.Session{}, fmt.Errorf("failed to get from db: %w", err)
	}

	// Convert result to Session
	bytes, _ := json.Marshal(res)
	var sessions []session.Session
	if err := json.Unmarshal(bytes, &sessions); err == nil && len(sessions) > 0 {
		s := sessions[0]
		m.l2.Set(id, s)
		m.l1.Set(id, s)
		return s, nil
	}

	return session.Session{}, fmt.Errorf("session not found")
}

func (m *Manager) Set(ctx context.Context, s session.Session) error {
	// 1. Write to DB (Strong Consistency)
	if _, err := m.db.Update(ctx, s.ID, s); err != nil {
		// Try Create if update fails (or use Upsert logic if available)
		if _, err := m.db.Create(ctx, "session", s); err != nil {
			return fmt.Errorf("failed to save to db: %w", err)
		}
	}

	// 2. Update L2
	if err := m.l2.Set(s.ID, s); err != nil {
		log.Printf("Failed to update L2: %v", err)
	}

	// 3. Update L1
	m.l1.Set(s.ID, s)

	// 4. Publish Event
	data, _ := json.Marshal(s)
	if err := m.eventBus.Publish(ctx, "session.updated", data); err != nil {
		log.Printf("Failed to publish event: %v", err)
	}

	return nil
}

func (m *Manager) DispatchTask(ctx context.Context, taskType string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return m.eventBus.Publish(ctx, fmt.Sprintf("task.%s", taskType), data)
}
