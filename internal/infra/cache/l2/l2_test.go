package l2

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	cache, err := NewCache[string](tempDir)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	if err := cache.Set(ctx, "key1", "hello", 10*time.Second); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	val, found, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found {
		t.Fatal("key1 not found")
	}
	if val != "hello" {
		t.Fatalf("val = %q, want hello", val)
	}
}

func TestCacheCorruptionRecovery(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Simulate database corruption by writing a corrupted SST/MANIFEST-like zero-filled file
	corruptedFile := filepath.Join(tempDir, "MANIFEST")
	if err := os.WriteFile(corruptedFile, make([]byte, 100), 0o644); err != nil {
		t.Fatalf("failed to create fake corrupted manifest file: %v", err)
	}

	// 2. Open the cache. Since we have corruption-recovery logic, it should automatically delete
	// the corrupted files, recreate the database, and open successfully.
	cache, err := NewCache[string](tempDir)
	if err != nil {
		t.Fatalf("NewCache() failed to recover and open: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	// 3. Verify the cache is fully functional.
	if err := cache.Set(ctx, "key-recovered", "success", 10*time.Second); err != nil {
		t.Fatalf("Set() failed after recovery: %v", err)
	}

	val, found, err := cache.Get(ctx, "key-recovered")
	if err != nil {
		t.Fatalf("Get() failed after recovery: %v", err)
	}
	if !found {
		t.Fatal("key not found after recovery")
	}
	if val != "success" {
		t.Fatalf("val = %q, want success", val)
	}
}
