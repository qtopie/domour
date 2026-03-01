package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tidwall/buntdb"
	"github.com/qtopie/homa/internal/assistant/plugins/copilot/shared"
)

type BuntDBStore struct {
	db         *buntdb.DB
	maxItems   int
	ttlSeconds int64
}

func NewBuntDBStore(path string, maxItems int, ttlSeconds int64) (*BuntDBStore, error) {
	db, err := buntdb.Open(path)
	if err != nil {
		return nil, err
	}
	return &BuntDBStore{db: db, maxItems: maxItems, ttlSeconds: ttlSeconds}, nil
}

func (s *BuntDBStore) key(sessionID string) string {
	return fmt.Sprintf("/sessions/%s/history", sessionID)
}

// AppendHistory appends a message to the session history and trims to maxItems.
func (s *BuntDBStore) AppendHistory(ctx context.Context, sessionID string, msg shared.Message) error {
	key := s.key(sessionID)
	return s.db.Update(func(tx *buntdb.Tx) error {
		var hist []shared.Message
		val, err := tx.Get(key)
		if err == nil {
			if jsonErr := json.Unmarshal([]byte(val), &hist); jsonErr != nil {
				hist = nil
			}
		} else if err != buntdb.ErrNotFound {
			return err
		}

		hist = append(hist, msg)
		if len(hist) > s.maxItems {
			hist = hist[len(hist)-s.maxItems:]
		}

		data, err := json.Marshal(hist)
		if err != nil {
			return err
		}

		var opts *buntdb.SetOptions
		if s.ttlSeconds > 0 {
			opts = &buntdb.SetOptions{Expires: true, TTL: time.Duration(s.ttlSeconds) * time.Second}
		}

		_, _, err = tx.Set(key, string(data), opts)
		return err
	})
}

// GetHistory returns up to maxItems recent messages for a session.
func (s *BuntDBStore) GetHistory(ctx context.Context, sessionID string) ([]shared.Message, error) {
	key := s.key(sessionID)
	var hist []shared.Message
	err := s.db.View(func(tx *buntdb.Tx) error {
		val, err := tx.Get(key)
		if err != nil {
			if err == buntdb.ErrNotFound {
				return nil
			}
			return err
		}
		return json.Unmarshal([]byte(val), &hist)
	})
	if err != nil {
		return nil, err
	}
	if len(hist) > s.maxItems {
		hist = hist[len(hist)-s.maxItems:]
	}
	return hist, nil
}

// Close closes underlying buntdb database.
func (s *BuntDBStore) Close() error {
	return s.db.Close()
}
