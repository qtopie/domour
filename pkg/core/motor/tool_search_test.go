package motor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchGrepTool(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "hello.txt")
	content := "Hello world\nThis is a test\nSniphunt is fast\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	if err := manager.Register(NewSearchGrepTool()); err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	t.Run("Basic search", func(t *testing.T) {
		result, err := manager.Execute(context.Background(), Command{
			Action: "search.grep",
			Input: map[string]interface{}{
				"pattern": "Sniphunt",
				"dir":     tmpDir,
			},
		})
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}

		if !strings.Contains(result.Observation, "Sniphunt is fast") {
			t.Errorf("expected observation to contain 'Sniphunt is fast', got: %q", result.Observation)
		}
		if !strings.Contains(result.Observation, "hello.txt:3") {
			t.Errorf("expected observation to contain file path and line number, got: %q", result.Observation)
		}
	})

	t.Run("No matches", func(t *testing.T) {
		result, err := manager.Execute(context.Background(), Command{
			Action: "search.grep",
			Input: map[string]interface{}{
				"pattern": "nonexistent",
				"dir":     tmpDir,
			},
		})
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}

		if result.Observation != "No matches found." {
			t.Errorf("expected 'No matches found.', got: %q", result.Observation)
		}
	})

	t.Run("Missing pattern", func(t *testing.T) {
		_, err := manager.Execute(context.Background(), Command{
			Action: "search.grep",
			Input: map[string]interface{}{
				"dir": tmpDir,
			},
		})
		if err == nil {
			t.Error("expected error for missing pattern, got nil")
		}
	})
}
