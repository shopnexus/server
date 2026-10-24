package staff_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
)

var discard = slog.New(slog.DiscardHandler)

// guarded serves 200 with the caller's staff ID behind Require(floor), at
// at.
func guarded(e env, floor member.Role, at time.Time) http.Handler {
	return staff.Require(e.stores.Sessions, func() time.Time { return at }, discard, floor)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, _ := authn.StaffFrom(r.Context())
			_, _ = io.WriteString(w, s.StaffID.String())
		}))
}

// get calls h with the session cookie tok, or none when tok is empty.
func get(t *testing.T, h http.Handler, tok string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/staff/me", http.NoBody)
	if tok != "" {
		r.AddCookie(&http.Cookie{Name: "staff_session", Value: tok})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRequireLetsALiveSessionThrough(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, in := e.enrolled(t, "mod@example.com")
	if w := get(t, guarded(e, member.RoleModerator, now), in.Token); w.Code != 200 || w.Body.String() != in.Me.ID.String() {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := get(t, guarded(e, member.RoleAdmin, now), in.Token); w.Code != 403 {
		t.Fatalf("a moderator on an admin route: %d", w.Code)
	}
	for name, tok := range map[string]string{"no cookie": "", "an unknown token": "ss_unknown"} {
		if w := get(t, guarded(e, member.RoleModerator, now), tok); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := get(t, guarded(e, member.RoleModerator, now.Add(staff.SessionTTL)), in.Token); w.Code != 401 {
		t.Fatalf("after eight hours: %d", w.Code)
	}
}

func TestRequireEndsWithDisable(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, in := e.enrolled(t, "mod@example.com")
	if _, err := staff.DisableMember(ctx, e.stores.Members, in.Me.ID, audit.ByCLI("test"), now); err != nil {
		t.Fatal(err)
	}
	if w := get(t, guarded(e, member.RoleModerator, now), in.Token); w.Code != 401 {
		t.Fatalf("after the disable: %d", w.Code)
	}
	if _, err := staff.EnableMember(ctx, e.stores.Members, in.Me.ID, audit.ByCLI("test"), now); err != nil {
		t.Fatal(err)
	}
	if w := get(t, guarded(e, member.RoleModerator, now), in.Token); w.Code != 401 {
		t.Fatalf("after enabling again: %d", w.Code)
	}
}

func TestFromConsole(t *testing.T) {
	t.Parallel()
	h := staff.FromConsole(discard)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for value, want := range map[string]int{"": 400, "XMLHttpRequest": 400, "shopnexus-console": 204} {
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/staff/login", http.NoBody)
		if value != "" {
			r.Header.Set("X-Requested-With", value)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%q: %d, want %d", value, w.Code, want)
		}
	}
}
