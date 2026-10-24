package staff

import (
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/session"
)

// Require lets a request through only for a live staff session whose member
// is active, at the session's credentials version, and holds at least
// floor's role. It reads the session and the member on every request, so a
// disable, a reset or a sign-out stops the next request.
func Require(sessions session.Store, k Keys, trusted []netip.Prefix, now func() time.Time, log *slog.Logger, floor member.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The scheme is case-insensitive (RFC 9110).
			scheme, tok, _ := strings.Cut(r.Header.Get("Authorization"), " ")
			if !strings.EqualFold(scheme, "Bearer") || tok == "" {
				httpx.WriteError(w, r, log, authn.ErrUnauthenticated)
				return
			}
			at := now()
			c, err := k.Verifier.Verify(tok, AudienceStaff, at)
			if err != nil {
				httpx.WriteError(w, r, log, err)
				return
			}
			if c.SessionID == (uuid.UUID{}) {
				httpx.WriteError(w, r, log, token.ErrInvalid)
				return
			}
			g, err := sessions.Grant(r.Context(), c.SessionID, c.Subject)
			if errors.Is(err, session.ErrNotFound) {
				httpx.WriteError(w, r, log, session.ErrRevoked)
				return
			}
			if err != nil {
				httpx.WriteError(w, r, log, err)
				return
			}
			if checkErr := g.Check(at); checkErr != nil {
				httpx.WriteError(w, r, log, checkErr)
				return
			}
			if !g.Role.AtLeast(floor) {
				httpx.WriteError(w, r, log, ErrStaffForbidden)
				return
			}
			caller := authn.Staff{StaffID: c.Subject, SessionID: c.SessionID, Role: string(g.Role), IP: httpx.ClientIP(r, trusted)}
			next.ServeHTTP(w, r.WithContext(authn.WithStaff(r.Context(), caller)))
		})
	}
}
