package session

import (
	"os"
	"path/filepath"
	"testing"
)

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
