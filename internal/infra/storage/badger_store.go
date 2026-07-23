package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/qtopie/domour/ark/session"
)

// BadgerStore persists session data in BadgerDB, surviving restarts.
type BadgerStore struct {
	db  *badger.DB
	ttl time.Duration
}

const defaultBadgerStorePath = "cache/domour/sessions"
const defaultSessionTTL = 7 * 24 * time.Hour

// NewBadgerStore opens or creates a BadgerDB-backed session store.
// If path is empty, defaults to ~/.domour/data/cache/domour/sessions.
func NewBadgerStore(path string) (*BadgerStore, error) {
	if path == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home dir: %w", err)
		}
		path = filepath.Join(homeDir, ".domour", "data", defaultBadgerStorePath)
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir badger store: %w", err)
	}

	db, err := openBadgerDB(path)
	if err != nil {
		return nil, err
	}

	return &BadgerStore{db: db, ttl: defaultSessionTTL}, nil
}

func openBadgerDB(path string) (*badger.DB, error) {
	if isBadgerCorrupted(path) {
		fmt.Printf("[BadgerStore] Detected corrupted DB at %s, recreating...\n", path)
		_ = os.RemoveAll(path)
		_ = os.MkdirAll(path, 0o755)
	}

	opts := badger.DefaultOptions(path)
	opts.Logger = nil

	// Reduce value log file size from default 1GB to 128MB.
	// Smaller mmap regions reduce the risk of SIGBUS when the underlying
	// file is truncated or damaged from a prior crash.
	opts = opts.WithValueLogFileSize(128 << 20) // 128MB

	// Limit memtable to 32MB to reduce in-memory mmap pressure.
	opts = opts.WithMemTableSize(32 << 20) // 32MB

	// Sync writes to disk before returning, reducing corruption window.
	opts = opts.WithSyncWrites(true)

	// Keep only the latest version of each key to reduce compaction overhead.
	opts = opts.WithNumVersionsToKeep(1)

	db, err := safeBadgerOpen(opts)
	if err != nil {
		// CRITICAL FIX: Do not delete directory if it's a lock contention issue!
		// Deleting the lock file while another process is running leads to concurrent writes and fatal SIGBUS.
		if strings.Contains(err.Error(), "lock") || strings.Contains(err.Error(), "resource temporarily unavailable") {
			return nil, fmt.Errorf("database is locked by another process: %w", err)
		}
		fmt.Printf("[BadgerStore] Failed to open DB at %s: %v. Recreating...\n", path, err)
		_ = os.RemoveAll(path)
		_ = os.MkdirAll(path, 0o755)
		db, err = safeBadgerOpen(opts)
		if err != nil {
			return nil, fmt.Errorf("open badger after recreate: %w", err)
		}
	}
	return db, nil
}

func safeBadgerOpen(opts badger.Options) (db *badger.DB, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("badger open panic: %v", r)
		}
	}()
	
	// Retry up to 3 seconds for lock acquisition (useful during Wails hot reloads)
	for i := 0; i < 30; i++ {
		db, err = badger.Open(opts)
		if err == nil {
			return db, nil
		}
		if !strings.Contains(err.Error(), "lock") && !strings.Contains(err.Error(), "resource temporarily unavailable") {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, err
}

func isBadgerCorrupted(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".sst") || strings.HasSuffix(name, ".vlog") || name == "MANIFEST" {
			info, err := entry.Info()
			if err != nil {
				return true
			}
			if info.Size() == 0 {
				return true
			}
		}
	}
	return false
}

// sessionKey returns the Badger key for a given session ID.
func sessionKey(sessionID string) []byte {
	return append([]byte("sess:"), []byte(sessionID)...)
}

func (s *BadgerStore) GetSession(_ context.Context, sessionID string) (sess session.Session, err error) {
	if s == nil || s.db == nil {
		return emptySession(sessionID), nil
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("badger read panic (SIGBUS): %v", r)
		}
	}()

	var raw []byte
	err = s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(sessionKey(sessionID))
		if err != nil {
			return err
		}
		raw, err = item.ValueCopy(nil)
		return err
	})
	if err == badger.ErrKeyNotFound {
		return emptySession(sessionID), nil
	}
	if err != nil {
		return session.Session{}, fmt.Errorf("read session %s: %w", sessionID, err)
	}

	if err := json.Unmarshal(raw, &sess); err != nil {
		return session.Session{}, fmt.Errorf("decode session %s: %w", sessionID, err)
	}
	return sess, nil
}

func (s *BadgerStore) SaveSession(_ context.Context, sess session.Session) (err error) {
	if s == nil || s.db == nil {
		return fmt.Errorf("badger store not initialized")
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("badger write panic (SIGBUS): %v", r)
		}
	}()

	sess.UpdatedAt = time.Now()
	data, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("encode session %s: %w", sess.ID, err)
	}

	return s.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry(sessionKey(sess.ID), data)
		if s.ttl > 0 {
			entry = entry.WithTTL(s.ttl)
		}
		return txn.SetEntry(entry)
	})
}

func (s *BadgerStore) AppendHistory(ctx context.Context, sessionID string, msg session.Message) error {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	if msg.Seq == 0 {
		var maxSeq int32
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

func (s *BadgerStore) GetHistory(ctx context.Context, sessionID string) ([]session.Message, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	result := make([]session.Message, len(sess.History))
	copy(result, sess.History)
	return result, nil
}

func (s *BadgerStore) ListSessions(_ context.Context) (list []session.Session, err error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("badger list panic (SIGBUS): %v", r)
		}
	}()

	err = s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("sess:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			val, err := item.ValueCopy(nil)
			if err != nil {
				continue
			}
			var sess session.Session
			if err := json.Unmarshal(val, &sess); err != nil {
				continue
			}
			list = append(list, sess)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *BadgerStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// RunGC periodically runs Badger value log GC to reclaim disk space and reduce
// the likelihood of file corruption. Should be called as a goroutine.
func (s *BadgerStore) RunGC(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Keep running GC until no more space can be reclaimed.
			for {
				err := s.db.RunValueLogGC(0.5)
				if err != nil {
					break
				}
			}
		}
	}
}

// Recover attempts to reopen the database after a crash, discarding corrupted data.
// This is useful when a panic recovery hook wants to restore the store to a usable state.
func (s *BadgerStore) Recover(path string) error {
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
	db, err := openBadgerDB(path)
	if err != nil {
		return err
	}
	s.db = db
	return nil
}

func emptySession(sessionID string) session.Session {
	return session.Session{
		ID:        sessionID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}
