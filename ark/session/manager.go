package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/qtopie/domour/ark/infra/cache"
	"github.com/qtopie/domour/ark/infra/eventbus"
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
	cache    *cache.Cache[string, Session]
	db       DB
	eventBus eventbus.EventBus
}

func NewManager(cacheStore *cache.Cache[string, Session], database DB, eb eventbus.EventBus) *Manager {
	return &Manager{
		cache:    cacheStore,
		db:       database,
		eventBus: eb,
	}
}

// GetHistory retrieves the conversation history for a session
func (m *Manager) GetHistory(ctx context.Context, sessionID string) ([]Message, error) {
	sess, err := m.GetSession(ctx, sessionID)
	if err != nil {
		// If session not found, return empty history (new session)
		return []Message{}, nil
	}
	return sess.History, nil
}

// AppendHistory appends a message to the session history
func (m *Manager) AppendHistory(ctx context.Context, sessionID string, msg Message) error {
	sess, err := m.GetSession(ctx, sessionID)
	if err != nil {
		// Create new session if not exists
		sess = Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			History:   []Message{},
		}
	}

	sess.History = append(sess.History, msg)
	sess.UpdatedAt = time.Now()

	return m.SaveSession(ctx, sess)
}

func (m *Manager) Close() error {
	if m.cache != nil {
		m.cache.Clear()
	}
	if m.db != nil {
		_ = m.db.Close()
	}
	return nil
}

func (m *Manager) GetSession(ctx context.Context, id string) (Session, error) {
	// 1. Check Cache
	if m.cache != nil {
		if s, ok := m.cache.Get(id); ok {
			return s, nil
		}
	}

	// 2. Check DB — pass RecordID object to handle hyphens and special characters
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
		if m.cache != nil {
			m.cache.Set(id, s)
		}
		return s, nil
	}

	var sessions []Session
	if err := json.Unmarshal(bytes, &sessions); err == nil && len(sessions) > 0 && len(sessions[0].ID) > 0 {
		s := sessions[0]
		if m.cache != nil {
			m.cache.Set(id, s)
		}
		return s, nil
	}

	var sess Session
	if err := json.Unmarshal(bytes, &sess); err == nil && sess.ID != "" {
		if m.cache != nil {
			m.cache.Set(id, sess)
		}
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

	// 2. Update Cache
	if m.cache != nil {
		m.cache.Set(s.ID, s)
	}

	// 3. Publish Event
	if m.eventBus != nil {
		data, _ := json.Marshal(s)
		if err := m.eventBus.Publish(ctx, "session.updated", data); err != nil {
			log.Printf("Failed to publish event: %v", err)
		}
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
	if m.eventBus == nil {
		return fmt.Errorf("eventBus is nil")
	}
	return m.eventBus.Publish(ctx, fmt.Sprintf("task.%s", taskType), data)
}
