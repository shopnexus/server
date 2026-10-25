package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/service"
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
// whose member holds at least floor's role; svc.Authenticate reads both on
// every request.
func Require(svc service.Service, log *slog.Logger, floor domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(sessionCookie)
			if err != nil || c.Value == "" {
				httpx.WriteError(w, r, log, authn.ErrUnauthenticated)
				return
			}
			g, err := svc.Authenticate(r.Context(), c.Value)
			if err != nil {
				httpx.WriteError(w, r, log, err)
				return
			}
			if !g.Role.AtLeast(floor) {
				httpx.WriteError(w, r, log, domain.ErrStaffForbidden)
				return
			}
			caller := authn.Staff{StaffID: g.StaffID, SessionID: g.SessionID, IP: httpx.ClientIP(r)}
			next.ServeHTTP(w, r.WithContext(authn.WithStaff(r.Context(), caller)))
		})
	}
}
