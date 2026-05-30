package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qtopie/domour/internal/infra/db"
	"github.com/qtopie/domour/internal/pkg/copilot/shared"
)

type SurrealStore struct {
	db *db.SurrealDB
}

func NewSurrealStore(db *db.SurrealDB) *SurrealStore {
	return &SurrealStore{db: db}
}

func (s *SurrealStore) GetSession(ctx context.Context, sessionID string) (Session, error) {
	res, err := s.db.Select(ctx, "session:"+sessionID)
	if err != nil {
		return Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}

	bytes, err := json.Marshal(res)
	if err != nil {
		return Session{}, err
	}

	var sessions []Session
	if err := json.Unmarshal(bytes, &sessions); err == nil && len(sessions) > 0 {
		return sessions[0], nil
	}

	var sess Session
	if err := json.Unmarshal(bytes, &sess); err == nil && sess.ID != "" {
		return sess, nil
	}

	return Session{
		ID:        sessionID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

func (s *SurrealStore) SaveSession(ctx context.Context, sess Session) error {
	sess.UpdatedAt = time.Now()
	if _, err := s.db.Update(ctx, "session:"+sess.ID, sess); err != nil {
		if _, err := s.db.Create(ctx, "session", sess); err != nil {
			return fmt.Errorf("failed to save to SurrealDB: %w", err)
		}
	}
	return nil
}

func (s *SurrealStore) AppendHistory(ctx context.Context, sessionID string, msg shared.Message) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
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
	return s.SaveSession(ctx, sess)
}

func (s *SurrealStore) GetHistory(ctx context.Context, sessionID string) ([]shared.Message, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return sess.History, nil
}

func (s *SurrealStore) Close() error {
	if s.db != nil {
		s.db.Close()
	}
	return nil
}
