package local

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLocalEventBus_PubSub(t *testing.T) {
	eb := NewEventBus()
	defer eb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	topic := "test.topic"
	expectedMsg := "hello eventbus"

	var wg sync.WaitGroup
	wg.Add(1)

	var receivedData string
	sub, err := eb.Subscribe(ctx, topic, func(data []byte) {
		receivedData = string(data)
		wg.Done()
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	err = eb.Publish(ctx, topic, []byte(expectedMsg))
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	wg.Wait()

	if receivedData != expectedMsg {
		t.Errorf("Expected %q, got %q", expectedMsg, receivedData)
	}

	// Test unsubscribe
	err = sub.Unsubscribe()
	if err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// Publish again, should not receive
	err = eb.Publish(ctx, topic, []byte("ignored message"))
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Wait a moment to ensure handler wasn't invoked
	time.Sleep(100 * time.Millisecond)
}
