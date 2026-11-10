package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
)

// Refresh rotates a refresh token. A token presented after it was rotated, or
// two refreshes racing with one token, means two parties hold it, so the
// session is revoked.
func (s *Account) Refresh(ctx context.Context, token string) (domain.Tokens, error) {
	now := s.now()
	hash := domain.HashRefresh(token)
	sess, err := s.sessions.FindByRefresh(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		prev, prevErr := s.sessions.FindByPrevRefresh(ctx, hash)
		if errors.Is(prevErr, repository.ErrNotFound) {
			return domain.Tokens{}, domain.ErrInvalidRefreshToken
		}
		if prevErr != nil {
			return domain.Tokens{}, fmt.Errorf("find session by previous token: %w", prevErr)
		}
		return domain.Tokens{}, s.reused(ctx, prev.ID(), now, "rotated token presented again")
	}
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("find session by token: %w", err)
	}
	if err = sess.Usable(now); err != nil {
		return domain.Tokens{}, fmt.Errorf("refresh: %w", err)
	}
	a, err := s.load(ctx, sess.AccountID())
	if err != nil {
		return domain.Tokens{}, err
	}
	// A suspended account's session ends with account_suspended; a deleted
	// one's, which a sign-in racing Erase may have issued, with
	// session_revoked.
	if r := a.Status(now).Refusal(); r != nil {
		if err = s.revoke(ctx, sess.ID(), now); err != nil {
			return domain.Tokens{}, err
		}
		return domain.Tokens{}, r
	}
	next := sess.Rotate(now)
	if err = s.sessions.Save(ctx, &sess, uuid.NewV7()); err != nil {
		if !errors.Is(err, repository.ErrConflict) {
			return domain.Tokens{}, fmt.Errorf("save session: %w", err)
		}
		// The write that won is a concurrent refresh with the same token, or a
		// sign-out. Only the first means two parties hold the token.
		cur, getErr := s.sessions.Get(ctx, sess.ID())
		if getErr != nil {
			return domain.Tokens{}, fmt.Errorf("get session: %w", getErr)
		}
		if cur.RevokedAt() != nil {
			return domain.Tokens{}, domain.ErrSessionRevoked
		}
		return domain.Tokens{}, s.reused(ctx, sess.ID(), now, "concurrent refresh")
	}
	tokens, err := s.signer.Tokens(sess, next, now)
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("issue tokens: %w", err)
	}
	return tokens, nil
}

// reused revokes a session whose refresh token two parties hold, records the
// security event and returns the refusal.
func (s *Account) reused(ctx context.Context, id uuid.UUID, now time.Time, why string) error {
	if err := s.revoke(ctx, id, now); err != nil {
		return err
	}
	s.log.WarnContext(ctx, "security: refresh token reuse, session revoked", "why", why, "session_id", id)
	refreshReuse.Add(ctx, 1)
	return domain.ErrSessionRevoked
}

// revoke reloads the session and revokes it, retrying once when a concurrent
// write gets there first.
func (s *Account) revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	var err error
	for range 2 {
		var sess domain.Session
		if sess, err = s.sessions.Get(ctx, id); err != nil {
			return fmt.Errorf("get session: %w", err)
		}
		if sess.RevokedAt() != nil {
			return nil
		}
		sess.Revoke(now)
		err = s.sessions.Save(ctx, &sess, uuid.NewV7())
		if err == nil {
			return nil
		}
		if !errors.Is(err, repository.ErrConflict) {
			return fmt.Errorf("save session: %w", err)
		}
	}
	return fmt.Errorf("revoke session: %w", err)
}
