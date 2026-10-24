package staff_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
)

var discard = slog.New(slog.DiscardHandler)

// guarded serves 200 with the caller's role behind Require(floor), at now.
func guarded(e env, floor member.Role, at time.Time) http.Handler {
	return staff.Require(e.stores.Sessions, e.keys, nil, func() time.Time { return at }, discard, floor)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, _ := authn.StaffFrom(r.Context())
			_, _ = io.WriteString(w, s.Role)
		}))
}

func get(t *testing.T, h http.Handler, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/staff/me", http.NoBody)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRequireLetsALiveSessionThrough(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, _, in := e.enrolled(t, "mod@example.com")
	if w := get(t, guarded(e, member.RoleModerator, now), in.SignIn.AccessToken); w.Code != 200 || w.Body.String() != "moderator" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := get(t, guarded(e, member.RoleAdmin, now), in.SignIn.AccessToken); w.Code != 403 {
		t.Fatalf("a moderator on an admin route: %d", w.Code)
	}
	if w := get(t, guarded(e, member.RoleModerator, now), ""); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
}

func TestRequireRefusesOtherTokens(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, temp := e.create(t, "mod@example.com", member.RoleModerator)
	step, err := staff.Login(context.Background(), e.stores.Members, e.stores.Quotas, e.keys, "mod@example.com", temp, clientIP, now)
	if err != nil {
		t.Fatal(err)
	}
	customer, _, _ := e.keys.Signer.Sign(token.Claims{
		Subject: uuid.NewV7(), SessionID: uuid.NewV7(), AuthTime: now, Version: 0, Audience: "shopnexus-app-test", ExpiresAt: time.Time{},
	}, time.Minute, now)
	noSession, _, _ := e.keys.Signer.Sign(token.Claims{
		Subject: uuid.NewV7(), SessionID: uuid.UUID{}, AuthTime: time.Time{}, Version: 0, Audience: staff.AudienceStaff, ExpiresAt: time.Time{},
	}, time.Minute, now)
	for name, tok := range map[string]string{"a login ticket": step.Ticket, "a customer token": customer, "no session": noSession} {
		if w := get(t, guarded(e, member.RoleModerator, now), tok); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
}
