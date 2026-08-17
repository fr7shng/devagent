package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TokenClaims struct {
	Caps      []string `json:"caps"`
	ExpiresAt int64    `json:"exp"`
}

type TokenManager struct {
	secret []byte
}

func NewTokenManager(secret string) *TokenManager {
	return &TokenManager{secret: []byte(secret)}
}

func GenerateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(i*7 + 13)
		}
	}
	return base64.StdEncoding.EncodeToString(b)
}

func (tm *TokenManager) Mint(caps []string, ttl time.Duration) (string, error) {
	claims := TokenClaims{
		Caps:      caps,
		ExpiresAt: time.Now().Add(ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	payloadB64 := base64.StdEncoding.EncodeToString(payload)

	mac := hmac.New(sha256.New, tm.secret)
	mac.Write([]byte(payloadB64))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s.%s", payloadB64, sig), nil
}

func (tm *TokenManager) Verify(token string) (*TokenClaims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid token format")
	}

	mac := hmac.New(sha256.New, tm.secret)
	mac.Write([]byte(parts[0]))
	expectedSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
		return nil, fmt.Errorf("invalid token signature")
	}

	payload, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims TokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

func (tm *TokenManager) CheckCap(claims *TokenClaims, capability string) bool {
	for _, c := range claims.Caps {
		if c == capability || c == "*" {
			return true
		}
	}
	return false
}
