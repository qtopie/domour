package storage

import (
	"context"
	"testing"
	"time"

	"github.com/qtopie/domour/ark/session"
	"github.com/qtopie/domour/internal/app/assistant/shared"
)

func TestMemoryStore_FullSession(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()
	sessionID := "test-session"

	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if sess.ID != sessionID {
		t.Errorf("expected session ID %s, got %s", sessionID, sess.ID)
	}

	msg1 := shared.Message{
		Role:    "user",
		Content: "Hello",
		Time:    time.Now().Unix(),
	}
	err = store.AppendHistory(ctx, sessionID, msg1)
	if err != nil {
		t.Fatalf("failed to append history: %v", err)
	}

	sess, _ = store.GetSession(ctx, sessionID)
	if len(sess.History) != 1 {
		t.Fatalf("expected history length 1, got %d", len(sess.History))
	}
	if sess.History[0].Seq != 1 {
		t.Errorf("expected sequence 1, got %d", sess.History[0].Seq)
	}

	sess.MemorySummary = "Summarized context"
	sess.ActiveProvider = "openai"
	sess.ActiveModel = "gpt-4o"
	sess.ProviderStats = map[string]*session.ProviderStat{
		"openai": {
			TokenUsed: 1500,
			CallCount: 2,
		},
	}

	err = store.SaveSession(ctx, sess)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	sess2, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if sess2.MemorySummary != "Summarized context" {
		t.Errorf("expected MemorySummary 'Summarized context', got %q", sess2.MemorySummary)
	}
	if sess2.ActiveProvider != "openai" {
		t.Errorf("expected ActiveProvider 'openai', got %q", sess2.ActiveProvider)
	}
	if sess2.ActiveModel != "gpt-4o" {
		t.Errorf("expected ActiveModel 'gpt-4o', got %q", sess2.ActiveModel)
	}
	if sess2.ProviderStats["openai"] == nil || sess2.ProviderStats["openai"].TokenUsed != 1500 {
		t.Errorf("expected ProviderStats tokens 1500, got %+v", sess2.ProviderStats["openai"])
	}
}
