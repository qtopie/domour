package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/qtopie/homa/internal/assistant/plugins/copilot/shared"
)

func TestBuntDBStore(t *testing.T) {
	dbPath := "test_sessions.db"
	defer os.Remove(dbPath)

	store, err := NewBuntDBStore(dbPath, 5, 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	sessionID := "sess-123"

	// Test empty history
	hist, err := store.GetHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to get history: %v", err)
	}
	if len(hist) != 0 {
		t.Errorf("expected empty history, got %d items", len(hist))
	}

	// Test append
	msg1 := shared.Message{Role: "user", Content: "hello", Time: time.Now().Unix()}
	if err := store.AppendHistory(ctx, sessionID, msg1); err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	hist, err = store.GetHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to get history: %v", err)
	}
	if len(hist) != 1 {
		t.Errorf("expected 1 item, got %d", len(hist))
	}
	if hist[0].Content != "hello" {
		t.Errorf("expected content 'hello', got %s", hist[0].Content)
	}

	// Test max items
	for i := 0; i < 10; i++ {
		msg := shared.Message{Role: "user", Content: "msg", Time: time.Now().Unix()}
		if err := store.AppendHistory(ctx, sessionID, msg); err != nil {
			t.Fatalf("failed to append loop: %v", err)
		}
	}

	hist, err = store.GetHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to get history: %v", err)
	}
	if len(hist) != 5 {
		t.Errorf("expected 5 items (maxItems), got %d", len(hist))
	}
}
