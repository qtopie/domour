package memory

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCache_RoundTrip(t *testing.T) {
	c := NewCache[string]()
	defer c.Close()

	ctx := context.Background()

	// Set & Get
	err := c.Set(ctx, "key1", "val1", 10*time.Second)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, found, err := c.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("key1 not found")
	}
	if val != "val1" {
		t.Errorf("expected val1, got %q", val)
	}

	// Delete
	err = c.Delete(ctx, "key1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, found, _ = c.Get(ctx, "key1")
	if found {
		t.Fatal("key1 still found after delete")
	}
}

func TestMemoryCache_TTL(t *testing.T) {
	c := NewCache[string]()
	defer c.Close()

	ctx := context.Background()

	// Set with short TTL
	err := c.Set(ctx, "key2", "val2", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Immediately check
	val, found, _ := c.Get(ctx, "key2")
	if !found || val != "val2" {
		t.Fatal("key2 should be found immediately")
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	_, found, _ = c.Get(ctx, "key2")
	if found {
		t.Fatal("key2 should have expired")
	}
}
