package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileEditTools(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Line 1\nLine 2\nLine 3\nLine 4\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	manager := NewManager(WithCleanupInterval(0))
	defer manager.Close()

	if err := manager.Register(NewFileEditLinesTool()); err != nil {
		t.Fatalf("failed to register edit_lines tool: %v", err)
	}
	if err := manager.Register(NewFileReplaceTool()); err != nil {
		t.Fatalf("failed to register replace tool: %v", err)
	}

	t.Run("Edit lines - middle", func(t *testing.T) {
		_, err := manager.Execute(context.Background(), Command{
			Action: "file.edit_lines",
			Input: map[string]interface{}{
				"path":       testFile,
				"start_line": 2,
				"end_line":   3,
				"content":    "New Line 2\nNew Line 3",
			},
		})
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}

		data, _ := os.ReadFile(testFile)
		expected := "Line 1\nNew Line 2\nNew Line 3\nLine 4\n"
		if string(data) != expected {
			t.Errorf("expected %q, got %q", expected, string(data))
		}
	})

	t.Run("Replace - unique", func(t *testing.T) {
		_, err := manager.Execute(context.Background(), Command{
			Action: "file.replace",
			Input: map[string]interface{}{
				"path": testFile,
				"old":  "Line 4",
				"new":  "Updated Line 4",
			},
		})
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}

		data, _ := os.ReadFile(testFile)
		if !strings.Contains(string(data), "Updated Line 4") {
			t.Errorf("expected observation to contain 'Updated Line 4', got: %q", string(data))
		}
	})

	t.Run("Replace - ambiguous", func(t *testing.T) {
		// Reset file to have duplicates
		os.WriteFile(testFile, []byte("Duplicate\nDuplicate\n"), 0644)
		_, err := manager.Execute(context.Background(), Command{
			Action: "file.replace",
			Input: map[string]interface{}{
				"path": testFile,
				"old":  "Duplicate",
				"new":  "Changed",
			},
		})
		if err == nil {
			t.Error("expected error for ambiguous replacement, got nil")
		}
		if !strings.Contains(err.Error(), "found 2 times") {
			t.Errorf("expected 'found 2 times' error, got: %v", err)
		}
	})

	t.Run("Edit lines - out of bounds", func(t *testing.T) {
		_, err := manager.Execute(context.Background(), Command{
			Action: "file.edit_lines",
			Input: map[string]interface{}{
				"path":       testFile,
				"start_line": 10,
				"end_line":   11,
				"content":    "Oops",
			},
		})
		if err == nil {
			t.Error("expected error for out of bounds line range, got nil")
		}
	})
}
