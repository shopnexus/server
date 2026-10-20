package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// Refresh rotates a refresh token. A token presented after it was rotated, or
// two refreshes racing with one token, means two parties hold it, so the
// session is revoked.
func Refresh(ctx context.Context, sessions session.Store, accounts AccountPort, signer session.Signer, token string, now time.Time, log *slog.Logger) (session.Tokens, error) {
	hash := session.HashRefresh(token)
	s, err := sessions.FindByRefresh(ctx, hash)
	if errors.Is(err, session.ErrNotFound) {
		prev, err := sessions.FindByPrevRefresh(ctx, hash)
		if errors.Is(err, session.ErrNotFound) {
			return session.Tokens{}, session.ErrInvalidRefresh
		}
		if err != nil {
			return session.Tokens{}, err
		}
		return session.Tokens{}, reused(ctx, sessions, prev.ID(), now, "rotated token presented again", log)
	}
	if err != nil {
		return session.Tokens{}, err
	}
	if err := s.Usable(now); err != nil {
		return session.Tokens{}, err
	}
	st, err := accounts.Status(ctx, s.AccountID())
	if err != nil {
		return session.Tokens{}, err
	}
	if st == account.StatusSuspended {
		if err := revoke(ctx, sessions, s.ID(), now); err != nil {
			return session.Tokens{}, err
		}
		return session.Tokens{}, account.ErrSuspended
	}
	next := s.Rotate(now)
	if err := sessions.Save(ctx, &s, uuid.NewV7()); err != nil {
		if errors.Is(err, session.ErrConflict) {
			return session.Tokens{}, reused(ctx, sessions, s.ID(), now, "concurrent refresh", log)
		}
		return session.Tokens{}, err
	}
	access, exp, err := signer.Sign(s.AccountID(), s.ID(), s.AuthTime(), now)
	if err != nil {
		return session.Tokens{}, err
	}
	return session.Tokens{AccessToken: access, AccessTokenExpiresAt: exp, RefreshToken: next, TokenType: "Bearer"}, nil
}

// reused revokes a session whose refresh token two parties hold, records the
// security event and returns the refusal.
func reused(ctx context.Context, sessions session.Store, id uuid.UUID, now time.Time, why string, log *slog.Logger) error {
	if err := revoke(ctx, sessions, id, now); err != nil {
		return err
	}
	log.WarnContext(ctx, "security: refresh token reuse, session revoked", "why", why, "session_id", id)
	refreshReuse.Add(ctx, 1)
	return session.ErrRevoked
}

// revoke reloads the session and revokes it, retrying once when a concurrent
// write gets there first.
func revoke(ctx context.Context, sessions session.Store, id uuid.UUID, now time.Time) error {
	var err error
	for range 2 {
		var s session.Session
		if s, err = sessions.Get(ctx, id); err != nil {
			return err
		}
		if s.RevokedAt() != nil {
			return nil
		}
		s.Revoke(now)
		if err = sessions.Save(ctx, &s, uuid.NewV7()); !errors.Is(err, session.ErrConflict) {
			return err
		}
	}
	return err
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// POST /v1/auth/refresh
func RefreshHandler(sessions session.Store, accounts AccountPort, signer session.Signer, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		in, err := httpx.Decode[RefreshRequest](r)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		out, err := Refresh(r.Context(), sessions, accounts, signer, in.RefreshToken, now(), log)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, r, http.StatusOK, out)
	})
}
