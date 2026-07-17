package memory

import (
	"context"
	"fmt"
	"sync"
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

func TestMemoryCache_GetNonExistent(t *testing.T) {
	c := NewCache[string]()
	defer c.Close()

	ctx := context.Background()
	_, found, err := c.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if found {
		t.Fatal("found should be false for non-existent key")
	}
}

func TestMemoryCache_NoTTL(t *testing.T) {
	c := NewCache[string]()
	defer c.Close()

	ctx := context.Background()
	if err := c.Set(ctx, "no-ttl", "value", 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, found, _ := c.Get(ctx, "no-ttl")
	if !found || val != "value" {
		t.Fatal("key with zero TTL should be available and not expire")
	}
}

func TestMemoryCache_DeleteNonExistent(t *testing.T) {
	c := NewCache[string]()
	defer c.Close()

	ctx := context.Background()
	if err := c.Delete(ctx, "nonexistent"); err != nil {
		t.Fatalf("Delete non-existent error: %v", err)
	}
}

func TestMemoryCache_Close(t *testing.T) {
	c := NewCache[string]()
	ctx := context.Background()

	if err := c.Set(ctx, "key1", "val1", 10*time.Second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	c.Close()

	_, found, _ := c.Get(ctx, "key1")
	if found {
		t.Fatal("key should not be found after close")
	}

	// Ensure double close does not panic
	c.Close()
}

func TestMemoryCache_ConcurrentAccess(t *testing.T) {
	c := NewCache[int]()
	defer c.Close()

	ctx := context.Background()
	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				key := fmt.Sprintf("key-%d", (id+j)%10)
				_ = c.Set(ctx, key, id*1000+j, 100*time.Millisecond)
				_, _, _ = c.Get(ctx, key)
			}
		}(i)
	}
	wg.Wait()
}

func TestMemoryCache_Overwrite(t *testing.T) {
	c := NewCache[string]()
	defer c.Close()

	ctx := context.Background()
	if err := c.Set(ctx, "key1", "first", 10*time.Second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := c.Set(ctx, "key1", "second", 10*time.Second); err != nil {
		t.Fatalf("Set overwrite failed: %v", err)
	}

	val, found, _ := c.Get(ctx, "key1")
	if !found || val != "second" {
		t.Fatalf("expected 'second', got %q (found=%v)", val, found)
	}
}

func TestMemoryCache_StructType(t *testing.T) {
	type config struct {
		Timeout int
		Retry   bool
	}

	c := NewCache[config]()
	defer c.Close()

	ctx := context.Background()
	original := config{Timeout: 30, Retry: true}
	if err := c.Set(ctx, "cfg", original, 10*time.Second); err != nil {
		t.Fatalf("Set struct failed: %v", err)
	}

	val, found, err := c.Get(ctx, "cfg")
	if err != nil {
		t.Fatalf("Get struct failed: %v", err)
	}
	if !found {
		t.Fatal("config not found")
	}
	if val.Timeout != 30 || val.Retry != true {
		t.Fatalf("got %+v, want {Timeout:30 Retry:true}", val)
	}
}
