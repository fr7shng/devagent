package daemon

import (
	"sync"
	"time"
)

type RateLimiter struct {
	buckets map[string]*bucket
	mu      sync.RWMutex
	rate    int
	window  time.Duration
}

type bucket struct {
	tokens   int
	lastTime time.Time
}

func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		window:  window,
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: rl.rate - 1, lastTime: time.Now()}
		return true
	}

	now := time.Now()
	elapsed := now.Sub(b.lastTime)
	if elapsed >= rl.window {
		b.tokens = rl.rate - 1
		b.lastTime = now
		return true
	}

	if b.tokens <= 0 {
		return false
	}

	b.tokens--
	b.lastTime = now
	return true
}
