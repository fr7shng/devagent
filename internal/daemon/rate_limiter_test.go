package daemon

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowWithinWindow(t *testing.T) {
	rl := NewRateLimiter(2, time.Second)
	if !rl.Allow("client_a") {
		t.Error("first request should be allowed")
	}
	if !rl.Allow("client_a") {
		t.Error("second request within window should be allowed")
	}
	if rl.Allow("client_a") {
		t.Error("third request should be blocked")
	}
}

func TestRateLimiter_PerClientBuckets(t *testing.T) {
	rl := NewRateLimiter(1, time.Second)
	if !rl.Allow("client_a") {
		t.Error("client_a first request should be allowed")
	}
	if rl.Allow("client_a") {
		t.Error("client_a second request should be blocked")
	}
	if !rl.Allow("client_b") {
		t.Error("client_b should have its own bucket")
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	rl := NewRateLimiter(1, 50*time.Millisecond)
	if !rl.Allow("client_a") {
		t.Error("first request should be allowed")
	}
	if rl.Allow("client_a") {
		t.Error("second request should be blocked")
	}
	time.Sleep(60 * time.Millisecond)
	if !rl.Allow("client_a") {
		t.Error("request after window expiry should be allowed")
	}
}
