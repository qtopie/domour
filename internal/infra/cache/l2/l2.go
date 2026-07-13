package l2

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type Cache[V any] struct {
	db *badger.DB
}

func safeBadgerOpen(opts badger.Options) (db *badger.DB, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("badger open panic: %v", r)
		}
	}()
	db, err = badger.Open(opts)
	return db, err
}

func NewCache[V any](path string) (*Cache[V], error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil

	// Use smaller value log files (128MB instead of 1GB) to reduce mmap region size,
	// minimizing the risk of SIGBUS from truncated/corrupted files.
	opts = opts.WithValueLogFileSize(128 << 20)

	// Sync writes to disk before returning, reducing the corruption window on crash.
	opts = opts.WithSyncWrites(true)

	// Keep only the latest version of each key.
	opts = opts.WithNumVersionsToKeep(1)

	db, err := safeBadgerOpen(opts)
	if err != nil {
		fmt.Printf("[L2Cache] Badger DB corrupted or failed to open: %v. Recreating...\n", err)
		_ = os.RemoveAll(path)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, err
		}
		db, err = safeBadgerOpen(opts)
		if err != nil {
			return nil, fmt.Errorf("failed to open badger db after recreate: %w", err)
		}
	}

	return &Cache[V]{
		db: db,
	}, nil
}

func (c *Cache[V]) Close() error {
	return c.db.Close()
}

func (c *Cache[V]) Set(ctx context.Context, key string, value V, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return c.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), data)
		if ttl > 0 {
			e = e.WithTTL(ttl)
		}
		return txn.SetEntry(e)
	})
}

func (c *Cache[V]) Get(ctx context.Context, key string) (V, bool, error) {
	var v V
	var valCopy []byte

	err := c.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		valCopy, err = item.ValueCopy(nil)
		return err
	})

	if err == badger.ErrKeyNotFound {
		return v, false, nil
	}
	if err != nil {
		return v, false, fmt.Errorf("failed to get key from badger: %w", err)
	}

	if err := json.Unmarshal(valCopy, &v); err != nil {
		return v, true, fmt.Errorf("failed to unmarshal value: %w", err)
	}

	return v, true, nil
}

func (c *Cache[V]) Delete(ctx context.Context, key string) error {
	return c.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}
