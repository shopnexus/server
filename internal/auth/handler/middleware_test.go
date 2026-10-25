package handler_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/handler"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/config"
)

var (
	now      = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	testKeys = func() config.AuthConfig {
		k := config.NewAuthKeys()
		k.Issuer, k.Audience = "server2-test", "shopnexus-app-test"
		return k
	}()
	discard = slog.New(slog.DiscardHandler)
)

func TestProtect(t *testing.T) {
	t.Parallel()
	acc, sid := uuid.NewV7(), uuid.NewV7()
	s, _ := domain.NewSession(sid, acc, "", now, now)
	valid, _, _ := domain.NewSigner(testKeys).Sign(s, now)
	expired, _, _ := domain.NewSigner(testKeys).Sign(s, now.Add(-20*time.Minute))
	var seen authn.Principal
	h := handler.Protect(domain.NewVerifier(testKeys), func() time.Time { return now }, discard)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen, _ = authn.From(r.Context())
			w.WriteHeader(http.StatusNoContent)
		}))
	for _, c := range []struct {
		header string
		status int
		code   string
	}{
		{"", 401, "unauthenticated"},
		{"Basic dXNlcjpwYXNz", 401, "unauthenticated"},
		{"Bearer ", 401, "unauthenticated"},
		{"Bearer junk", 401, "invalid_token"},
		{"Bearer " + expired, 401, "invalid_token"},
		{"Bearer " + valid, 204, ""},
		{"bearer " + valid, 204, ""}, // the scheme is case-insensitive (RFC 9110)
		{"BEARER " + valid, 204, ""},
		{"Bearer" + valid, 401, "unauthenticated"},
	} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", http.NoBody)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != c.status || body.Error.Code != c.code {
			t.Errorf("%q: %d %q", c.header, rec.Code, body.Error.Code)
		}
	}
	if seen.AccountID != acc || seen.SessionID != sid {
		t.Fatalf("principal %+v", seen)
	}
}
