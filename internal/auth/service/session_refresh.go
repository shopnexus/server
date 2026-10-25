package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
)

// Refresh rotates a refresh token. A token presented after it was rotated, or
// two refreshes racing with one token, means two parties hold it, so the
// session is revoked.
func (a *Auth) Refresh(ctx context.Context, token string) (domain.Tokens, error) {
	now := a.now()
	hash := domain.HashRefresh(token)
	s, err := a.sessions.FindByRefresh(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		prev, prevErr := a.sessions.FindByPrevRefresh(ctx, hash)
		if errors.Is(prevErr, repository.ErrNotFound) {
			return domain.Tokens{}, domain.ErrInvalidRefresh
		}
		if prevErr != nil {
			return domain.Tokens{}, fmt.Errorf("find session by previous token: %w", prevErr)
		}
		return domain.Tokens{}, a.reused(ctx, prev.ID(), now, "rotated token presented again")
	}
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("find session by token: %w", err)
	}
	if err = s.Usable(now); err != nil {
		return domain.Tokens{}, fmt.Errorf("refresh: %w", err)
	}
	st, err := a.accounts.Status(ctx, s.AccountID())
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("get account status: %w", err)
	}
	if st == accountdomain.StatusSuspended {
		if err = a.revoke(ctx, s.ID(), now); err != nil {
			return domain.Tokens{}, err
		}
		return domain.Tokens{}, accountdomain.ErrSuspended
	}
	next := s.Rotate(now)
	if err = a.sessions.Save(ctx, &s, uuid.NewV7()); err != nil {
		if !errors.Is(err, repository.ErrConflict) {
			return domain.Tokens{}, fmt.Errorf("save session: %w", err)
		}
		// The write that won is a concurrent refresh with the same token, or a
		// sign-out. Only the first means two parties hold the token.
		cur, getErr := a.sessions.Get(ctx, s.ID())
		if getErr != nil {
			return domain.Tokens{}, fmt.Errorf("get session: %w", getErr)
		}
		if cur.RevokedAt() != nil {
			return domain.Tokens{}, domain.ErrRevoked
		}
		return domain.Tokens{}, a.reused(ctx, s.ID(), now, "concurrent refresh")
	}
	tokens, err := a.signer.Tokens(s, next, now)
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("issue tokens: %w", err)
	}
	return tokens, nil
}

// reused revokes a session whose refresh token two parties hold, records the
// security event and returns the refusal.
func (a *Auth) reused(ctx context.Context, id uuid.UUID, now time.Time, why string) error {
	if err := a.revoke(ctx, id, now); err != nil {
		return err
	}
	a.log.WarnContext(ctx, "security: refresh token reuse, session revoked", "why", why, "session_id", id)
	refreshReuse.Add(ctx, 1)
	return domain.ErrRevoked
}

// revoke reloads the session and revokes it, retrying once when a concurrent
// write gets there first.
func (a *Auth) revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	var err error
	for range 2 {
		var s domain.Session
		if s, err = a.sessions.Get(ctx, id); err != nil {
			return fmt.Errorf("get session: %w", err)
		}
		if s.RevokedAt() != nil {
			return nil
		}
		s.Revoke(now)
		err = a.sessions.Save(ctx, &s, uuid.NewV7())
		if err == nil {
			return nil
		}
		if !errors.Is(err, repository.ErrConflict) {
			return fmt.Errorf("save session: %w", err)
		}
	}
	return fmt.Errorf("revoke session: %w", err)
}
