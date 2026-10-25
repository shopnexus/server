package handler_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/handler"
	"github.com/shopnexus/server2/internal/staff/service"
)

var discard = slog.New(slog.DiscardHandler)

// sessions knows one token, a moderator's.
type sessions struct {
	service.Service

	token string
	grant domain.Grant
}

func (s sessions) Authenticate(_ context.Context, tok string) (domain.Grant, error) {
	if tok != s.token {
		return domain.Grant{}, authn.ErrUnauthenticated
	}
	return s.grant, nil
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

func TestRequire(t *testing.T) {
	t.Parallel()
	svc := sessions{token: "ss_mod", grant: domain.Grant{SessionID: uuid.NewV7(), StaffID: uuid.NewV7(), Role: domain.RoleModerator}}
	guarded := func(floor domain.Role) http.Handler {
		return handler.Require(svc, discard, floor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, _ := authn.StaffFrom(r.Context())
			_, _ = io.WriteString(w, s.StaffID.String())
		}))
	}
	if w := get(t, guarded(domain.RoleModerator), "ss_mod"); w.Code != 200 || w.Body.String() != svc.grant.StaffID.String() {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := get(t, guarded(domain.RoleAdmin), "ss_mod"); w.Code != 403 {
		t.Fatalf("a moderator on an admin route: %d", w.Code)
	}
	for name, tok := range map[string]string{"no cookie": "", "an unknown token": "ss_unknown"} {
		if w := get(t, guarded(domain.RoleModerator), tok); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
}

func TestFromConsole(t *testing.T) {
	t.Parallel()
	h := handler.FromConsole(discard)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
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
