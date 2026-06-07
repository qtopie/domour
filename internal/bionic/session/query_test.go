package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtopie/domour/internal/app/assistant/shared"
	"github.com/qtopie/domour/internal/infra/storage"
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
	sess1 := Session{
		ID:             "session-openai",
		ActiveProvider: "openai",
		ActiveModel:    "gpt-4",
		UpdatedAt:      time.Now().Add(-10 * time.Minute),
		History: []shared.Message{
			{Role: "user", Content: "Hello OpenAI", Time: time.Now().Unix(), Seq: 1},
			{Role: "assistant", Content: "Hello user", Time: time.Now().Unix(), Seq: 2},
		},
	}
	_ = store.SaveSession(ctx, sess1)

	// 2. Create a session with provider gemini
	sess2 := Session{
		ID:             "session-gemini",
		ActiveProvider: "gemini",
		ActiveModel:    "gemini-1.5-pro",
		UpdatedAt:      time.Now(),
		History: []shared.Message{
			{Role: "user", Content: "Hello Gemini", Time: time.Now().Unix(), Seq: 1},
			{Role: "assistant", Content: "Hello user from Gemini", Time: time.Now().Unix(), Seq: 2},
		},
	}
	_ = store.SaveSession(ctx, sess2)

	// 3. Query all sessions
	results, err := QuerySessions(ctx, store, QueryFilter{})
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
	resultsFiltered, err := QuerySessions(ctx, store, QueryFilter{Provider: "gemini"})
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
	resultsSess, err := QuerySessions(ctx, store, QueryFilter{SessionID: "session-openai"})
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

func TestParseCliSession(t *testing.T) {
	// Create a temp directory
	tempDir, err := os.MkdirTemp("", "chats-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	jsonlPath := filepath.Join(tempDir, "session-test.jsonl")

	// Write mock jsonl content
	mockContent := `{"sessionId":"mock-cli-session","projectHash":"hash","startTime":"2026-05-31T20:00:00Z","lastUpdated":"2026-05-31T20:05:00Z","kind":"main"}
{"id":"u1","timestamp":"2026-05-31T20:00:05Z","type":"user","content":[{"text":"[USER]\nUser request:\nShow me the money"}]}
{"id":"g1","timestamp":"2026-05-31T20:00:10Z","type":"gemini","content":"Here is your money","model":"gemini-pro"}
`
	if err := os.WriteFile(jsonlPath, []byte(mockContent), 0o644); err != nil {
		t.Fatalf("failed to write mock jsonl: %v", err)
	}

	info, err := parseCliSession(jsonlPath)
	if err != nil {
		t.Fatalf("failed to parse cli session: %v", err)
	}

	if info.SessionID != "mock-cli-session" {
		t.Errorf("expected sessionId 'mock-cli-session', got %s", info.SessionID)
	}

	if len(info.History) != 2 {
		t.Fatalf("expected history length 2, got %d", len(info.History))
	}

	if info.History[0].Role != "user" || info.History[0].Content != "Show me the money" {
		t.Errorf("unexpected first message: %+v", info.History[0])
	}

	if info.History[1].Role != "assistant" || info.History[1].Content != "Here is your money" {
		t.Errorf("unexpected second message: %+v", info.History[1])
	}

	if info.Model != "gemini-pro" {
		t.Errorf("expected model 'gemini-pro', got %s", info.Model)
	}
}
