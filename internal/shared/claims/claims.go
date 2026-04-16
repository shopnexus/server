package authclaims

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/cache"
	accountmodel "shopnexus-server/internal/module/account/model"

	"github.com/golang-jwt/jwt/v5"
)

const (
	tokenHeader        = "Authorization"
	tokenPrefix        = "Bearer "
	tokenCacheDuration = 5 * 60 * time.Second
)

var claimsCache = cache.NewInMemoryClient()

// GetClaimsByHeader retrieves and validates JWT claims from the token, using an in-memory cache.
func GetClaimsByHeader(header http.Header) (accountmodel.Claims, error) {
	token := header.Get(tokenHeader)

	if token == "" {
		return accountmodel.Claims{}, errors.New("missing authorization header")
	}

	// Try to get claims from cache first
	var claims accountmodel.Claims
	if err := claimsCache.Get(context.Background(), token, &claims); err == nil {
		return claims, nil
	}

	// If not in cache, validate token and store in cache
	claims, err := ValidateAccessToken(config.GetConfig().App.JWT.Secret, strings.TrimPrefix(token, tokenPrefix))
	if err != nil {
		return accountmodel.Claims{}, err
	}

	// Store claims in cache
	if err = claimsCache.Set(context.Background(), token, claims, tokenCacheDuration); err != nil {
		return accountmodel.Claims{}, err
	}

	return claims, nil
}

func GetClaims(r *http.Request) (accountmodel.Claims, error) {
	return GetClaimsByHeader(r.Header)
}

func ValidateAccessToken(secret string, tokenStr string) (claims accountmodel.Claims, err error) {
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	})

	if err != nil {
		return claims, err
	}

	if !token.Valid {
		return claims, errors.New("invalid token or token expired")
	}

	return claims, nil
}
