package brain

import (
	"testing"
	"time"
)

func TestRateLimiter_ExactCapacity(t *testing.T) {
	rl := NewRateLimiter(15) // 15 RPM

	// All 15 initial tokens should be available
	for i := 0; i < 15; i++ {
		if !rl.Allow() {
			t.Fatalf("Call %d should be allowed (within 15 RPM)", i+1)
		}
	}

	// 16th should be blocked
	if rl.Allow() {
		t.Fatal("Call 16 should be blocked (exceeded 15 RPM)")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(60) // 60 RPM = 1 token/sec

	// Drain all tokens
	for i := 0; i < 60; i++ {
		rl.Allow()
	}

	// After 1 second, 1 token should refill
	time.Sleep(1100 * time.Millisecond)
	if !rl.Allow() {
		t.Fatal("After 1.1s should have 1 token available")
	}

	// Next immediate call should be blocked
	if rl.Allow() {
		t.Fatal("After using the refilled token, next should be blocked")
	}

	// After 2 more seconds, ~2 tokens available
	time.Sleep(2100 * time.Millisecond)
	if !rl.Allow() {
		t.Fatal("After 2.1s should have 2 tokens — first should succeed")
	}
	if !rl.Allow() {
		t.Fatal("After 2.1s should have 2 tokens — second should succeed")
	}
	if rl.Allow() {
		t.Fatal("After using 2 refilled tokens, third should be blocked")
	}
}

func TestRateLimiter_NoLimit(t *testing.T) {
	// When not set, rate limiter is nil — nothing to test here.
	// Verify that WithRateLimit(0) does not create a limiter.
	d := NewDiencephalonNode(WithRateLimit(0))
	if d.rateLimiter != nil {
		t.Fatal("WithRateLimit(0) should not create a rate limiter")
	}

	d2 := NewDiencephalonNode(WithRateLimit(-1))
	if d2.rateLimiter != nil {
		t.Fatal("WithRateLimit(-1) should not create a rate limiter")
	}
}

func TestRateLimiter_Wait(t *testing.T) {
	rl := NewRateLimiter(120) // 120 RPM = 2 tokens/sec

	// Drain all tokens
	for i := 0; i < 120; i++ {
		rl.Allow()
	}

	// Wait should block for ~500ms (1 token at 2/sec)
	start := time.Now()
	rl.Wait()
	elapsed := time.Since(start)
	if elapsed < 400*time.Millisecond {
		t.Fatalf("Wait should block for ~500ms, got %v", elapsed)
	}
}
