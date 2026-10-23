package auth

import (
	"context"
	"errors"
	"fmt"
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
	if err := sessions.RevokeAll(ctx, p.AccountID, now); err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	return nil
}

// RemoveSession signs one of the caller's devices out.
func RemoveSession(ctx context.Context, sessions session.Store, p authn.Principal, id uuid.UUID, now time.Time) error {
	s, err := sessions.Get(ctx, id)
	if errors.Is(err, session.ErrNotFound) {
		return ErrSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if s.AccountID() != p.AccountID {
		return ErrSessionNotFound
	}
	return revoke(ctx, sessions, id, now)
}

// LogoutHandler serves POST /v1/auth/logout.
func LogoutHandler(sessions session.Store, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		if err := Logout(r.Context(), sessions, p, now()); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// LogoutAllHandler serves POST /v1/auth/logout-all.
func LogoutAllHandler(sessions session.Store, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		if err := LogoutAll(r.Context(), sessions, p, now()); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

type RemoveSessionRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// RemoveSessionHandler serves DELETE /v1/auth/sessions/{id}.
func RemoveSessionHandler(sessions session.Store, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		var in RemoveSessionRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		if err := RemoveSession(r.Context(), sessions, p, in.ID, now()); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
