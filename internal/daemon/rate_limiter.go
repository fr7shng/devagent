package daemon

import (
	"sync"
	"time"
)

type RateLimiter struct {
	buckets   map[string]*bucket
	mu        sync.RWMutex
	rate      int
	window    time.Duration
	lastSweep time.Time
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

	now := time.Now()
	if rl.lastSweep.IsZero() || now.Sub(rl.lastSweep) > rl.window {
		rl.sweep(now)
		rl.lastSweep = now
	}

	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &bucket{tokens: rl.rate - 1, lastTime: now}
		return true
	}

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

// sweep 清理超过两个窗口未访问的桶，防止 map 随客户端 IP 无限增长。
func (rl *RateLimiter) sweep(now time.Time) {
	cutoff := now.Add(-2 * rl.window)
	for k, b := range rl.buckets {
		if b.lastTime.Before(cutoff) {
			delete(rl.buckets, k)
		}
	}
}
