package staff

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
)

// FromConsole refuses a request without X-Requested-With:
// shopnexus-console. A form or a link on another site cannot set the
// header, so no cross-site request rides the session cookie. The spec
// requires the header too; this keeps it required if the spec ever forgets.
func FromConsole(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Requested-With") != "shopnexus-console" {
				httpx.WriteError(w, r, log, ErrNotFromConsole)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Require lets a request through only with the cookie of a live session
// whose member holds at least floor's role. It reads the session and the
// member on every request, so a disable, a reset or a sign-out stops the
// next request.
func Require(sessions SessionStore, now func() time.Time, log *slog.Logger, floor member.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(sessionCookie)
			if err != nil || c.Value == "" {
				httpx.WriteError(w, r, log, authn.ErrUnauthenticated)
				return
			}
			g, err := sessions.Live(r.Context(), token.Hash(c.Value), now())
			if errors.Is(err, errSessionNotFound) {
				httpx.WriteError(w, r, log, authn.ErrUnauthenticated)
				return
			}
			if err != nil {
				httpx.WriteError(w, r, log, err)
				return
			}
			if !g.Role.AtLeast(floor) {
				httpx.WriteError(w, r, log, ErrStaffForbidden)
				return
			}
			caller := authn.Staff{StaffID: g.StaffID, SessionID: g.SessionID, IP: httpx.ClientIP(r)}
			next.ServeHTTP(w, r.WithContext(authn.WithStaff(r.Context(), caller)))
		})
	}
}
