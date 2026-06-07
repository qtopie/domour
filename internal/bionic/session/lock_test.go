package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLocalLocker_Serialization(t *testing.T) {
	locker := NewLocalLocker()
	ctx := context.Background()
	sessionID := "sess-1"

	var order []int
	var mu sync.Mutex

	// Acquire lock 1
	unlock1, err := locker.Lock(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to acquire lock 1: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		// Acquire lock 2 (should block until unlock1 is called)
		unlock2, err := locker.Lock(ctx, sessionID)
		if err != nil {
			t.Errorf("failed to acquire lock 2: %v", err)
			return
		}
		defer unlock2()

		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
	}()

	// Ensure goroutine has started and is waiting
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	order = append(order, 1)
	mu.Unlock()

	unlock1()

	wg.Wait()

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("unexpected execution order: %v", order)
	}
}

func TestLocalLocker_ContextCancellation(t *testing.T) {
	locker := NewLocalLocker()
	sessionID := "sess-2"

	// Acquire lock 1
	unlock1, err := locker.Lock(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("failed to acquire lock 1: %v", err)
	}
	defer unlock1()

	// Try to acquire lock 2 with a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = locker.Lock(ctx, sessionID)
	if err == nil {
		t.Error("expected error due to cancelled context, got nil")
	}
}
