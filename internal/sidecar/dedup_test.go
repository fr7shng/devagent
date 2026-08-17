package sidecar

import (
	"testing"
	"time"
)

func TestDedupWindow_AllowNew(t *testing.T) {
	dw := NewDedupWindow()
	key := "shelf_01:set_relay:{pin:1,state:true}"
	if !dw.Allow(key) {
		t.Error("first call should be allowed")
	}
}

func TestDedupWindow_BlockDuplicate(t *testing.T) {
	dw := NewDedupWindow()
	key := "shelf_01:set_relay:{pin:1,state:true}"
	dw.Allow(key)
	if dw.Allow(key) {
		t.Error("duplicate within window should be blocked")
	}
}

func TestDedupWindow_Expire(t *testing.T) {
	dw := NewDedupWindow()
	dw.ttl = 100 * time.Millisecond
	key := "shelf_01:set_relay:{pin:1,state:true}"
	dw.Allow(key)
	time.Sleep(150 * time.Millisecond)
	if !dw.Allow(key) {
		t.Error("expired key should be allowed again")
	}
}
