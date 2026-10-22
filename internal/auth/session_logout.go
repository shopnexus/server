package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

var (
	ErrReauthRequired = apperr.New(apperr.KindPreconditionRequired, "reauth_required", "sign in again to do this")
	// ErrSessionNotFound also answers for another account's session: a
	// resource the caller may not see is 404, never 403.
	ErrSessionNotFound = apperr.New(apperr.KindNotFound, "session_not_found", "no such session")
)

// recentAuth is how long after a sign-in the sensitive actions stay open.
const recentAuth = 15 * time.Minute

// Logout revokes the caller's session. Its access token keeps working until
// it expires; the app discards it.
func Logout(ctx context.Context, sessions session.Store, p authn.Principal, now time.Time) error {
	return revoke(ctx, sessions, p.SessionID, now)
}

// LogoutAll revokes every session of the caller's account after a recent
// sign-in.
func LogoutAll(ctx context.Context, sessions session.Store, p authn.Principal, now time.Time) error {
	if now.Sub(p.AuthTime) > recentAuth {
		return ErrReauthRequired
	}
	return sessions.RevokeAll(ctx, p.AccountID, now)
}

// RemoveSession signs one of the caller's devices out.
func RemoveSession(ctx context.Context, sessions session.Store, p authn.Principal, id uuid.UUID, now time.Time) error {
	s, err := sessions.Get(ctx, id)
	if errors.Is(err, session.ErrNotFound) {
		return ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	if s.AccountID() != p.AccountID {
		return ErrSessionNotFound
	}
	return revoke(ctx, sessions, id, now)
}

// LogoutHandler serves POST /v1/auth/logout.
func LogoutHandler(sessions session.Store, now func() time.Time, log *slog.Logger) http.Handler {
	return noContent(log, func(r *http.Request, p authn.Principal) error { return Logout(r.Context(), sessions, p, now()) })
}

// LogoutAllHandler serves POST /v1/auth/logout-all.
func LogoutAllHandler(sessions session.Store, now func() time.Time, log *slog.Logger) http.Handler {
	return noContent(log, func(r *http.Request, p authn.Principal) error { return LogoutAll(r.Context(), sessions, p, now()) })
}

// RemoveSessionHandler serves DELETE /v1/auth/sessions/{id}.
func RemoveSessionHandler(sessions session.Store, now func() time.Time, log *slog.Logger) http.Handler {
	return noContent(log, func(r *http.Request, p authn.Principal) error {
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			return ErrSessionNotFound
		}
		return RemoveSession(r.Context(), sessions, p, id, now())
	})
}

// noContent runs a signed-in action and answers 204. It takes a function
// because it is HTTP plumbing; the use cases above take none.
func noContent(log *slog.Logger, do func(*http.Request, authn.Principal) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		if err := do(r, p); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
