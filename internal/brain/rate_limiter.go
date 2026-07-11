package brain

import (
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter.
// Thread-safe for concurrent access across sessions.
type RateLimiter struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// NewRateLimiter creates a rate limiter with the given RPM.
func NewRateLimiter(rpm int) *RateLimiter {
	return &RateLimiter{
		tokens:     float64(rpm),
		maxTokens:  float64(rpm),
		refillRate: float64(rpm) / 60.0, // tokens per second
		lastRefill: time.Now(),
	}
}

// Allow checks if a request can proceed without blocking.
// Returns true if the request is allowed, false if rate limited.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.lastRefill = now

	// Refill tokens
	rl.tokens += elapsed * rl.refillRate
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}

	if rl.tokens >= 1.0 {
		rl.tokens--
		return true
	}
	return false
}

// Wait blocks until a token is available.
func (rl *RateLimiter) Wait() {
	for !rl.Allow() {
		time.Sleep(100 * time.Millisecond)
	}
}
