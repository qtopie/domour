package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qtopie/domour/ark/session"
	"github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"
)

func sessionRID(id string) models.RecordID {
	return models.NewRecordID("session", id)
}

type SurrealStore struct {
	db *SurrealDB
}

func NewSurrealStore(db *SurrealDB) *SurrealStore {
	return &SurrealStore{db: db}
}

func (s *SurrealStore) GetSession(ctx context.Context, sessionID string) (session.Session, error) {
	res, err := s.db.SelectWithRecordID(ctx, sessionRID(sessionID))
	if err != nil {
		return session.Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}

	bytes, err := json.Marshal(res)
	if err != nil {
		return session.Session{}, err
	}

	var sess session.Session
	if err := json.Unmarshal(bytes, &sess); err == nil && sess.ID != "" {
		return sess, nil
	}

	var queryResults []surrealdb.QueryResult[any]
	if err := json.Unmarshal(bytes, &queryResults); err == nil && len(queryResults) > 0 {
		resultBytes, _ := json.Marshal(queryResults[0].Result)
		if string(resultBytes) != "null" {
			if err := json.Unmarshal(resultBytes, &sess); err == nil && sess.ID != "" {
				return sess, nil
			}
		}
	}

	return session.Session{
		ID:        sessionID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

func (s *SurrealStore) SaveSession(ctx context.Context, sess session.Session) error {
	sess.UpdatedAt = time.Now()
	_, err := s.db.Upsert(ctx, sessionRID(sess.ID), sess)
	if err != nil {
		if _, err2 := s.db.Create(ctx, "session", sess); err2 != nil {
			return fmt.Errorf("failed to save to SurrealDB: %w", err2)
		}
	}
	return nil
}

func (s *SurrealStore) AppendHistory(ctx context.Context, sessionID string, msg session.Message) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

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

func (s *SurrealStore) GetHistory(ctx context.Context, sessionID string) ([]session.Message, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return sess.History, nil
}

func (s *SurrealStore) ListSessions(ctx context.Context) ([]session.Session, error) {
	res, err := s.db.Select(ctx, "session")
	if err != nil {
		return nil, err
	}

	bytes, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}

	var sessions []session.Session
	if err := json.Unmarshal(bytes, &sessions); err != nil {
		return nil, err
	}

	return sessions, nil
}

func (s *SurrealStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
