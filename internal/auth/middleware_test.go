package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/authn"
)

func TestRequireAuth(t *testing.T) {
	acc, sid := uuid.NewV7(), uuid.NewV7()
	s, _ := session.New(sid, acc, "", now, now)
	valid, _, _ := session.NewSigner(testKeys).Sign(s, now)
	expired, _, _ := session.NewSigner(testKeys).Sign(s, now.Add(-20*time.Minute))
	var seen authn.Principal
	h := auth.RequireAuth(session.NewVerifier(testKeys), func() time.Time { return now }, discard)(
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
		r := httptest.NewRequest("GET", "/x", nil)
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
