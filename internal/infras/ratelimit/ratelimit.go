// Package ratelimit provides a Redis-backed implementation of Echo's
// middleware.RateLimiterStore interface plus a small Factory to construct
// pre-configured middlewares for per-route limits.
//
// Why Redis: in a multi-instance deployment each process has its own memory,
// so Echo's in-memory store enforces limit*N across the cluster. Redis gives
// us a shared counter so the quota is the real thing.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"shopnexus-server/internal/infras/cache"
	authclaims "shopnexus-server/internal/shared/claims"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/rueidis"
	"golang.org/x/time/rate"
)

// redisStore implements middleware.RateLimiterStore with a fixed-window counter.
// Key layout: "ratelimit:<scope>:<identifier>". TTL is set via EXPIRE NX so it
// only anchors on the first hit of a window — subsequent hits within the window
// keep the existing TTL, matching fixed-window semantics.
type redisStore struct {
	client rueidis.Client
	scope  string
	limit  int64
	window time.Duration
}

func (s *redisStore) Allow(identifier string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	key := fmt.Sprintf("ratelimit:%s:%s", s.scope, identifier)
	incr := s.client.B().Incr().Key(key).Build()
	expire := s.client.B().Expire().Key(key).Seconds(int64(s.window.Seconds())).Nx().Build()

	resps := s.client.DoMulti(ctx, incr, expire)
	count, err := resps[0].AsInt64()
	if err != nil {
		return false, err
	}
	return count <= s.limit, nil
}

// Factory produces Echo rate limit middlewares backed by a shared Redis client.
// Falls back to Echo's memory store if Redis is not configured (dev/test).
type Factory struct {
	client rueidis.Client // nil = fall back to memory store
}

// NewFactory wires a Factory from the shared cache.Client. If the client is a
// RedisClient, its rueidis handle is reused; otherwise the factory produces
// memory-backed middlewares (single-instance only).
func NewFactory(c cache.Client) *Factory {
	if rc, ok := c.(*cache.RedisClient); ok {
		return &Factory{client: rc.Client}
	}
	return &Factory{}
}

// Middleware returns an echo.MiddlewareFunc that enforces `limit` requests per
// `window` per (scope + actor). Actor is the authenticated account ID if
// available, otherwise the client's IP address.
//
// Example: f.Middleware("checkout", 10, time.Minute) allows any user 10
// checkouts per minute.
func (f *Factory) Middleware(scope string, limit int64, window time.Duration) echo.MiddlewareFunc {
	var store middleware.RateLimiterStore
	if f.client != nil {
		store = &redisStore{client: f.client, scope: scope, limit: limit, window: window}
	} else {
		// Token-bucket approximation: refill rate = limit/window, burst = limit
		// (so up to `limit` requests back-to-back, then slow refill). Memory
		// store is per-process — acceptable for dev / single instance.
		store = middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:  rate.Limit(float64(limit) / window.Seconds()),
				Burst: int(limit),
			},
		)
	}

	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: store,
		IdentifierExtractor: func(c echo.Context) (string, error) {
			if claims, err := authclaims.GetClaims(c.Request()); err == nil {
				return claims.Account.ID.String(), nil
			}
			return c.RealIP(), nil
		},
	})
}
