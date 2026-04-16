package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"shopnexus-server/internal/infras/ratelimit"
)

// TestFactoryMemoryFallback verifies that when no Redis client is available,
// the factory falls back to Echo's in-memory store and the middleware still
// works end-to-end via a real Echo handler.
//
// The memory store is a token-bucket; rate is expressed as (limit / window).
// With limit=3 over 1 minute, burst of 3 is allowed, 4th request gets 429.
func TestFactoryMemoryFallback(t *testing.T) {
	// Nil cache → factory uses memory store
	f := ratelimit.NewFactory(nil)

	e := echo.New()
	e.GET("/x", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}, f.Middleware("test", 3, time.Minute))

	// Send 3 requests from the same IP — all should succeed
	for i := 1; i <= 3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 OK, got %d (body=%q)", i, rec.Code, rec.Body.String())
		}
	}

	// 4th request must be rate-limited
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th request: expected 429, got %d (body=%q)", rec.Code, rec.Body.String())
	}
}

// TestFactoryIsolatesIPs verifies different clients get independent quotas
// even when they hit the same route (same scope).
func TestFactoryIsolatesIPs(t *testing.T) {
	f := ratelimit.NewFactory(nil)
	e := echo.New()
	e.GET("/x", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}, f.Middleware("iso", 1, time.Minute))

	// IP A first request: OK
	recA1 := httptest.NewRecorder()
	reqA := httptest.NewRequest(http.MethodGet, "/x", nil)
	reqA.RemoteAddr = "10.0.0.1:1000"
	e.ServeHTTP(recA1, reqA)
	if recA1.Code != http.StatusOK {
		t.Fatalf("IP A first: expected 200, got %d", recA1.Code)
	}

	// IP A second request: blocked
	recA2 := httptest.NewRecorder()
	reqA2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	reqA2.RemoteAddr = "10.0.0.1:1000"
	e.ServeHTTP(recA2, reqA2)
	if recA2.Code != http.StatusTooManyRequests {
		t.Fatalf("IP A second: expected 429, got %d", recA2.Code)
	}

	// IP B first request: still OK (independent bucket)
	recB := httptest.NewRecorder()
	reqB := httptest.NewRequest(http.MethodGet, "/x", nil)
	reqB.RemoteAddr = "10.0.0.2:2000"
	e.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("IP B first: expected 200, got %d (IP-isolation broken)", recB.Code)
	}
}
