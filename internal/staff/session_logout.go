package staff

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/session"
)

// Logout signs the caller's session out.
func Logout(ctx context.Context, sessions session.Store, caller authn.Staff, now time.Time) error {
	return revoke(ctx, sessions, caller.SessionID, nil, now)
}

// LogoutHandler serves POST /v1/staff/session/logout and clears the cookie.
func LogoutHandler(sessions session.Store, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := httpx.StaffCaller(w, r, log)
		if !ok {
			return
		}
		if err := Logout(r.Context(), sessions, caller, now()); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		clearRefreshCookie(w)
		w.WriteHeader(http.StatusNoContent)
	})
}
