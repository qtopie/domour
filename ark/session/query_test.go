package session_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/qtopie/domour/internal/infra/storage"
	"github.com/qtopie/domour/ark/session"
)

func TestQuerySessions_MemoryAndFilter(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cli-roots-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	os.Setenv("DOMOUR_TEST_CLI_ROOTS", tempDir)
	defer os.Unsetenv("DOMOUR_TEST_CLI_ROOTS")

	store := storage.NewMemoryStore()
	defer store.Close()

	ctx := context.Background()

	// 1. Create a session with provider openai
	sess1 := session.Session{
		ID:             "session-openai",
		ActiveProvider: "openai",
		ActiveModel:    "gpt-4",
		UpdatedAt:      time.Now().Add(-10 * time.Minute),
		History: []session.Message{
			{Role: "user", Content: "Hello OpenAI", Time: time.Now().Unix(), Seq: 1},
			{Role: "assistant", Content: "Hello user", Time: time.Now().Unix(), Seq: 2},
		},
	}
	_ = store.SaveSession(ctx, sess1)

	// 2. Create a session with provider gemini
	sess2 := session.Session{
		ID:             "session-gemini",
		ActiveProvider: "gemini",
		ActiveModel:    "gemini-1.5-pro",
		UpdatedAt:      time.Now(),
		History: []session.Message{
			{Role: "user", Content: "Hello Gemini", Time: time.Now().Unix(), Seq: 1},
			{Role: "assistant", Content: "Hello user from Gemini", Time: time.Now().Unix(), Seq: 2},
		},
	}
	_ = store.SaveSession(ctx, sess2)

	// 3. Query all sessions
	results, err := session.QuerySessions(ctx, store, session.QueryFilter{})
	if err != nil {
		t.Fatalf("failed to query sessions: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(results))
	}

	// First element should be session-gemini because it has a newer UpdatedAt
	if results[0].SessionID != "session-gemini" {
		t.Errorf("expected newest session first ('session-gemini'), got %s", results[0].SessionID)
	}

	// 4. Query with provider filter (gemini)
	resultsFiltered, err := session.QuerySessions(ctx, store, session.QueryFilter{Provider: "gemini"})
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	if len(resultsFiltered) != 1 {
		t.Errorf("expected 1 gemini session, got %d", len(resultsFiltered))
	}
	if resultsFiltered[0].SessionID != "session-gemini" {
		t.Errorf("expected 'session-gemini', got %s", resultsFiltered[0].SessionID)
	}

	// 5. Query with session ID filter
	resultsSess, err := session.QuerySessions(ctx, store, session.QueryFilter{SessionID: "session-openai"})
	if err != nil {
		t.Fatalf("failed to query by ID: %v", err)
	}
	if len(resultsSess) != 1 {
		t.Errorf("expected 1 session, got %d", len(resultsSess))
	}
	if resultsSess[0].SessionID != "session-openai" {
		t.Errorf("expected 'session-openai', got %s", resultsSess[0].SessionID)
	}
}
