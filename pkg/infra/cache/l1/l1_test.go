package l1

import (
	"testing"
	"time"
)

func TestL1CacheRoundTrip(t *testing.T) {
	c, err := NewCache[string, string](100, 10*time.Second)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}

	c.Set("key1", "val1")
	val, found := c.Get("key1")
	if !found {
		t.Fatal("key1 not found")
	}
	if val != "val1" {
		t.Fatalf("expected 'val1', got %q", val)
	}
}

func TestL1CacheGetNonExistent(t *testing.T) {
	c, err := NewCache[string, string](100, 10*time.Second)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}

	_, found := c.Get("nonexistent")
	if found {
		t.Fatal("found should be false for non-existent key")
	}
}

func TestL1CacheDelete(t *testing.T) {
	c, err := NewCache[string, string](100, 10*time.Second)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}

	c.Set("key1", "val1")
	c.Delete("key1")

	_, found := c.Get("key1")
	if found {
		t.Fatal("key1 should not be found after delete")
	}
}

func TestL1CacheClear(t *testing.T) {
	c, err := NewCache[string, string](100, 10*time.Second)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}

	c.Set("key1", "val1")
	c.Set("key2", "val2")
	c.Clear()

	_, found1 := c.Get("key1")
	_, found2 := c.Get("key2")
	if found1 || found2 {
		t.Fatal("keys should not be found after clear")
	}
}

func TestL1CacheOverwrite(t *testing.T) {
	c, err := NewCache[string, string](100, 10*time.Second)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}

	c.Set("key1", "first")
	c.Set("key1", "second")

	val, found := c.Get("key1")
	if !found || val != "second" {
		t.Fatalf("expected 'second', got %q (found=%v)", val, found)
	}
}

func TestL1CacheIntKey(t *testing.T) {
	c, err := NewCache[int, string](100, 10*time.Second)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}

	c.Set(42, "answer")
	val, found := c.Get(42)
	if !found || val != "answer" {
		t.Fatalf("expected 'answer', got %q (found=%v)", val, found)
	}
}

func TestL1CacheZeroCapacity(t *testing.T) {
	_, err := NewCache[string, string](0, 10*time.Second)
	if err == nil {
		t.Fatal("expected error for zero capacity")
	}
}
