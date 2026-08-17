package sidecar

import (
	"testing"
	"time"
)

func TestTokenMintAndVerify(t *testing.T) {
	tm := NewTokenManager("test-secret")
	token, err := tm.Mint([]string{"lamp.write", "lamp.read"}, time.Hour)
	if err != nil {
		t.Fatalf("mint failed: %v", err)
	}
	claims, err := tm.Verify(token)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if len(claims.Caps) != 2 {
		t.Errorf("expected 2 caps, got %d", len(claims.Caps))
	}
}

func TestTokenExpired(t *testing.T) {
	tm := NewTokenManager("test-secret")
	token, _ := tm.Mint([]string{"lamp.write"}, -1*time.Second)
	_, err := tm.Verify(token)
	if err == nil {
		t.Error("expected expired error")
	}
}

func TestTokenInvalidSignature(t *testing.T) {
	tm1 := NewTokenManager("secret-a")
	tm2 := NewTokenManager("secret-b")
	token, _ := tm1.Mint([]string{"lamp.write"}, time.Hour)
	_, err := tm2.Verify(token)
	if err == nil {
		t.Error("expected signature mismatch error")
	}
}

func TestCheckCap(t *testing.T) {
	tm := NewTokenManager("test-secret")
	token, _ := tm.Mint([]string{"lamp.write", "lamp.read"}, time.Hour)
	claims, _ := tm.Verify(token)
	if !tm.CheckCap(claims, "lamp.write") {
		t.Error("expected lamp.write to be allowed")
	}
	if tm.CheckCap(claims, "motor.write") {
		t.Error("expected motor.write to be denied")
	}
}

func TestCheckCapWildcard(t *testing.T) {
	tm := NewTokenManager("test-secret")
	token, _ := tm.Mint([]string{"*"}, time.Hour)
	claims, _ := tm.Verify(token)
	if !tm.CheckCap(claims, "anything") {
		t.Error("wildcard should allow any capability")
	}
}
