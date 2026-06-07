package l2

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type Cache[V any] struct {
	db  *badger.DB
	ttl time.Duration
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

func NewCache[V any](path string, ttl time.Duration) (*Cache[V], error) {
	opts := badger.DefaultOptions(path)
	// Suppress verbose logging
	opts.Logger = nil

	db, err := safeBadgerOpen(opts)
	if err != nil {
		// Cache is corrupted or panicked. Automatically delete and recreate it.
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
		db:  db,
		ttl: ttl,
	}, nil
}

func (c *Cache[V]) Close() error {
	return c.db.Close()
}

func (c *Cache[V]) Set(key string, value V) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return c.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), data).WithTTL(c.ttl)
		return txn.SetEntry(e)
	})
}

func (c *Cache[V]) Get(key string) (V, bool, error) {
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
		return v, true, fmt.Errorf("failed to unmarshal value: %w", err) // true because key was found
	}

	return v, true, nil
}

func (c *Cache[V]) Delete(key string) error {
	return c.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}
