package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverHarnessPath(t *testing.T) {
	// Preserve existing env var
	oldEnv := os.Getenv("ANTIGRAVITY_HARNESS_PATH")
	defer func() {
		os.Setenv("ANTIGRAVITY_HARNESS_PATH", oldEnv)
	}()

	t.Run("from environment variable", func(t *testing.T) {
		os.Setenv("ANTIGRAVITY_HARNESS_PATH", "/mock/env/path")
		path := discoverHarnessPath(&Config{BaseURL: "/mock/config/path"})
		if path != "/mock/env/path" {
			t.Errorf("expected /mock/env/path, got %s", path)
		}
	})

	t.Run("from config file BaseURL when env var is empty", func(t *testing.T) {
		os.Setenv("ANTIGRAVITY_HARNESS_PATH", "")
		path := discoverHarnessPath(&Config{BaseURL: "/mock/config/path"})
		if path != "/mock/config/path" {
			t.Errorf("expected /mock/config/path, got %s", path)
		}
	})

	t.Run("from sibling directories lookup", func(t *testing.T) {
		os.Setenv("ANTIGRAVITY_HARNESS_PATH", "")
		
		// Create a temporary mock directory structure
		tempDir, err := os.MkdirTemp("", "agy-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tempDir)

		// Create workspace structure:
		// tempDir/projects/domour (cwd)
		// tempDir/projects/antigravity-sdk-python/localharness
		projectsDir := filepath.Join(tempDir, "projects")
		domourDir := filepath.Join(projectsDir, "domour")
		harnessDir := filepath.Join(projectsDir, "antigravity-sdk-python", "localharness")

		if err := os.MkdirAll(domourDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(harnessDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Change CWD to the mock domour dir
		oldCwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(domourDir); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = os.Chdir(oldCwd)
		}()

		path := discoverHarnessPath(nil)
		// Evaluated path could be a resolved symlink, so compare using filepath.EvalSymlinks or similar,
		// but simple comparison usually works.
		evalPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			evalPath = path
		}
		evalHarnessDir, err := filepath.EvalSymlinks(harnessDir)
		if err != nil {
			evalHarnessDir = harnessDir
		}

		if evalPath != evalHarnessDir {
			t.Errorf("expected %s, got %s", evalHarnessDir, evalPath)
		}
	})
}
