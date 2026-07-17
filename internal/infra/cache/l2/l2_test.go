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

func TestCacheGetNonExistent(t *testing.T) {
	tempDir := t.TempDir()
	cache, err := NewCache[string](tempDir)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()
	_, found, err := cache.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if found {
		t.Fatal("found should be false for non-existent key")
	}
}

func TestCacheDelete(t *testing.T) {
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

	if err := cache.Delete(ctx, "key1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, found, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get() after delete error = %v", err)
	}
	if found {
		t.Fatal("key should not be found after delete")
	}
}

func TestCacheTTL(t *testing.T) {
	tempDir := t.TempDir()
	cache, err := NewCache[string](tempDir)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	// Set with a moderate TTL
	if err := cache.Set(ctx, "key_ttl", "expires_soon", 200*time.Millisecond); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Should still be available immediately
	val, found, err := cache.Get(ctx, "key_ttl")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found || val != "expires_soon" {
		t.Fatal("key_ttl should be available immediately")
	}

	// Wait for TTL to expire (badger checks TTL on read)
	time.Sleep(500 * time.Millisecond)

	_, found, err = cache.Get(ctx, "key_ttl")
	if err != nil {
		t.Fatalf("Get() after TTL error = %v", err)
	}
	if found {
		t.Fatal("key_ttl should have expired")
	}
}

func TestCacheSetOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	cache, err := NewCache[string](tempDir)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	if err := cache.Set(ctx, "key1", "first", 10*time.Second); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := cache.Set(ctx, "key1", "second", 10*time.Second); err != nil {
		t.Fatalf("Set() overwrite error = %v", err)
	}

	val, found, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found || val != "second" {
		t.Fatalf("expected 'second', got %q (found=%v)", val, found)
	}
}

func TestCacheDeleteNonExistent(t *testing.T) {
	tempDir := t.TempDir()
	cache, err := NewCache[string](tempDir)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()
	if err := cache.Delete(ctx, "nonexistent"); err != nil {
		t.Fatalf("Delete() non-existent error = %v", err)
	}
}

func TestCacheStructType(t *testing.T) {
	type testStruct struct {
		Name  string
		Value int
	}

	tempDir := t.TempDir()
	cache, err := NewCache[testStruct](tempDir)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()
	original := testStruct{Name: "test", Value: 42}
	if err := cache.Set(ctx, "struct-key", original, 10*time.Second); err != nil {
		t.Fatalf("Set() struct error = %v", err)
	}

	val, found, err := cache.Get(ctx, "struct-key")
	if err != nil {
		t.Fatalf("Get() struct error = %v", err)
	}
	if !found {
		t.Fatal("struct-key not found")
	}
	if val.Name != "test" || val.Value != 42 {
		t.Fatalf("got %+v, want {Name:test Value:42}", val)
	}
}

func TestCacheNoTTL(t *testing.T) {
	tempDir := t.TempDir()
	cache, err := NewCache[string](tempDir)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()
	if err := cache.Set(ctx, "no-ttl-key", "persistent", 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	val, found, err := cache.Get(ctx, "no-ttl-key")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found || val != "persistent" {
		t.Fatalf("expected 'persistent', got %q (found=%v)", val, found)
	}
}
