package daemon

import (
	"github.com/ng/devagent/internal/auth"
)

type TokenManager = auth.TokenManager
type TokenClaims = auth.TokenClaims

func NewTokenManager(secret string) *TokenManager {
	return auth.NewTokenManager(secret)
}
