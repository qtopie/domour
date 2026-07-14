package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/pkg/infra/cache"
	"github.com/qtopie/domour/pkg/infra/cache/l1"
	"github.com/qtopie/domour/pkg/infra/eventbus"
	"github.com/surrealdb/surrealdb.go/pkg/models"
)

// DB defines the database interface needed by the session manager.
type DB interface {
	Query(ctx context.Context, query string, vars map[string]any) (any, error)
	Create(ctx context.Context, table string, data any) (any, error)
	Update(ctx context.Context, id string, data any) (any, error)
	Close() error
}

type Manager struct {
	l1       *l1.Cache[string, Session]
	l2       cache.Cache[Session]
	db       DB
	eventBus eventbus.EventBus
}

func NewManager(l1Cache *l1.Cache[string, Session], l2Cache cache.Cache[Session], database DB, eb eventbus.EventBus) *Manager {
	return &Manager{
		l1:       l1Cache,
		l2:       l2Cache,
		db:       database,
		eventBus: eb,
	}
}

// GetHistory retrieves the conversation history for a session
func (m *Manager) GetHistory(ctx context.Context, sessionID string) ([]shared.Message, error) {
	sess, err := m.GetSession(ctx, sessionID)
	if err != nil {
		// If session not found, return empty history (new session)
		return []shared.Message{}, nil
	}
	return sess.History, nil
}

// AppendHistory appends a message to the session history
func (m *Manager) AppendHistory(ctx context.Context, sessionID string, msg shared.Message) error {
	sess, err := m.GetSession(ctx, sessionID)
	if err != nil {
		// Create new session if not exists
		sess = Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			History:   []shared.Message{},
		}
	}

	sess.History = append(sess.History, msg)
	sess.UpdatedAt = time.Now()

	return m.SaveSession(ctx, sess)
}

func (m *Manager) Close() error {
	m.l1.Clear()
	if m.l2 != nil {
		_ = m.l2.Close()
	}
	if m.db != nil {
		_ = m.db.Close()
	}
	return nil
}

func (m *Manager) GetSession(ctx context.Context, id string) (Session, error) {
	// 1. Check L1
	if s, ok := m.l1.Get(id); ok {
		return s, nil
	}

	// 2. Check L2
	if s, ok, err := m.l2.Get(ctx, id); err == nil && ok {
		m.l1.Set(id, s)
		return s, nil
	} else if err != nil {
		log.Printf("L2 cache error: %v", err)
	}

	// 3. Check DB — pass RecordID object to handle hyphens and special characters
	rid := models.NewRecordID("session", id)
	res, err := m.db.Query(ctx, "SELECT * FROM $id", map[string]any{"id": rid})
	if err != nil {
		return Session{}, fmt.Errorf("failed to get from db: %w", err)
	}

	// Convert result to Session
	bytes, _ := json.Marshal(res)
	
	// If query returns a SurrealDB query result wrapper, try to unmarshal that first
	var queryResults []struct {
		Result []Session `json:"result"`
	}
	if err := json.Unmarshal(bytes, &queryResults); err == nil && len(queryResults) > 0 && len(queryResults[0].Result) > 0 && len(queryResults[0].Result[0].ID) > 0 {
		s := queryResults[0].Result[0]
		m.l2.Set(ctx, id, s, 24*time.Hour) // Set default L2 TTL if needed
		m.l1.Set(id, s)
		return s, nil
	}

	var sessions []Session
	if err := json.Unmarshal(bytes, &sessions); err == nil && len(sessions) > 0 && len(sessions[0].ID) > 0 {
		s := sessions[0]
		m.l2.Set(ctx, id, s, 24*time.Hour)
		m.l1.Set(id, s)
		return s, nil
	}

	var sess Session
	if err := json.Unmarshal(bytes, &sess); err == nil && sess.ID != "" {
		m.l2.Set(ctx, id, sess, 24*time.Hour)
		m.l1.Set(id, sess)
		return sess, nil
	}

	return Session{}, fmt.Errorf("session not found")
}

func (m *Manager) SaveSession(ctx context.Context, s Session) error {
	if s.ID == "" {
		return fmt.Errorf("cannot save session with empty ID")
	}

	dbID := "session:" + s.ID

	// 1. Write to DB (Strong Consistency)
	if _, err := m.db.Update(ctx, dbID, s); err != nil {
		// Try Create if update fails
		if _, err := m.db.Create(ctx, "session", s); err != nil {
			return fmt.Errorf("failed to save to db: %w", err)
		}
	}

	// 2. Update L2
	if err := m.l2.Set(ctx, s.ID, s, 24*time.Hour); err != nil {
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

func (m *Manager) ListSessions(ctx context.Context) ([]Session, error) {
	res, err := m.db.Query(ctx, "SELECT * FROM session", nil)
	if err != nil {
		return nil, err
	}
	bytes, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	
	var queryResults []struct {
		Result []Session `json:"result"`
	}
	if err := json.Unmarshal(bytes, &queryResults); err == nil && len(queryResults) > 0 {
		return queryResults[0].Result, nil
	}
	
	var sessions []Session
	if err := json.Unmarshal(bytes, &sessions); err == nil {
		return sessions, nil
	}
	
	return nil, fmt.Errorf("failed to unmarshal sessions")
}

func (m *Manager) DispatchTask(ctx context.Context, taskType string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return m.eventBus.Publish(ctx, fmt.Sprintf("task.%s", taskType), data)
}
