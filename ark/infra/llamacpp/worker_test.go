package llamacpp

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestWorker_ConcurrencyAndCancellation verifies [SPEC-LLAMA-004]
func TestWorker_ConcurrencyAndCancellation(t *testing.T) {
	engine, err := NewEngine(Config{UseStub: true})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer func() { _ = engine.Close() }()

	const numConcurrent = 20
	var wg sync.WaitGroup
	errCh := make(chan error, numConcurrent)

	// Concurrently invoke ForwardLogits and Generate
	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if idx%2 == 0 {
				resp, err := engine.ForwardLogits(ctx, ForwardRequest{
					Prompt:        "hello world concurrent task",
					MarkerIndices: []int{0, 1},
				})
				if err != nil {
					errCh <- err
					return
				}
				if resp == nil || len(resp.Logits) != 2 {
					errCh <- err
				}
			} else {
				resp, err := engine.Generate(ctx, GenerateRequest{
					Prompt:    "hello concurrent generation",
					MaxTokens: 4,
				})
				if err != nil {
					errCh <- err
					return
				}
				if resp == nil || resp.Content == "" {
					errCh <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent worker job failed: %v", err)
		}
	}

	// Test Context Cancellation
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = engine.Generate(cancelledCtx, GenerateRequest{
		Prompt: "should not be generated",
	})
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}
