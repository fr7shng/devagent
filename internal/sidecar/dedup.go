package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type DedupWindow struct {
	seen map[string]time.Time
	ttl  time.Duration
	mu   sync.Mutex
}

func NewDedupWindow() *DedupWindow {
	return NewDedupWindowWithTTL(3 * time.Second)
}

func NewDedupWindowWithTTL(ttl time.Duration) *DedupWindow {
	dw := &DedupWindow{
		seen: make(map[string]time.Time),
		ttl:  ttl,
	}
	return dw
}

func (dw *DedupWindow) StartCleanup(ctx context.Context) {
	go dw.cleanup(ctx)
}

func (dw *DedupWindow) Allow(key string) bool {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	if t, ok := dw.seen[key]; ok && time.Since(t) < dw.ttl {
		return false
	}
	dw.seen[key] = time.Now()
	return true
}

func (dw *DedupWindow) MakeKey(deviceID, capability string, params any) string {
	paramsJSON, _ := json.Marshal(params)
	return fmt.Sprintf("%s:%s:%s", deviceID, capability, string(paramsJSON))
}

func (dw *DedupWindow) cleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dw.mu.Lock()
			now := time.Now()
			for k, t := range dw.seen {
				if now.Sub(t) > dw.ttl {
					delete(dw.seen, k)
				}
			}
			dw.mu.Unlock()
		}
	}
}
