package session

import (
	"context"
	"sync"
)

// Locker provides a lock interface for session-based request serialization.
type Locker interface {
	Lock(ctx context.Context, sessionID string) (unlock func(), err error)
}

// LocalLocker is an in-memory, thread-safe session locker that serializes requests
// based on SessionID. It cleans up unused resources dynamically.
type LocalLocker struct {
	mu    sync.Mutex
	locks map[string]*lockRef
}

type lockRef struct {
	sem chan struct{}
	ref int
}

// NewLocalLocker creates a new LocalLocker instance.
func NewLocalLocker() *LocalLocker {
	return &LocalLocker{
		locks: make(map[string]*lockRef),
	}
}

// Lock blocks until the lock for sessionID is acquired, or the context is cancelled.
// It returns a non-nil unlock function on success.
func (l *LocalLocker) Lock(ctx context.Context, sessionID string) (func(), error) {
	l.mu.Lock()
	ref, ok := l.locks[sessionID]
	if !ok {
		ref = &lockRef{
			sem: make(chan struct{}, 1),
		}
		// Initialize the semaphore as unlocked (has 1 token)
		ref.sem <- struct{}{}
		l.locks[sessionID] = ref
	}
	ref.ref++
	l.mu.Unlock()

	select {
	case <-ctx.Done():
		l.mu.Lock()
		ref.ref--
		if ref.ref == 0 {
			delete(l.locks, sessionID)
		}
		l.mu.Unlock()
		return nil, ctx.Err()
	case <-ref.sem:
		// Acquired the lock!
	}

	once := sync.Once{}
	unlock := func() {
		once.Do(func() {
			l.mu.Lock()
			ref.ref--
			if ref.ref == 0 {
				delete(l.locks, sessionID)
			} else {
				// Relinquish the lock to the next waiting goroutine
				ref.sem <- struct{}{}
			}
			l.mu.Unlock()
		})
	}
	return unlock, nil
}
