package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
)

// Refresh rotates a refresh token. A token presented after it was rotated, or
// two refreshes racing with one token, means two parties hold it, so the
// session is revoked. A suspended account's session is revoked too, and
// its token answers account_suspended while the suspension lasts.
func (s *Account) Refresh(ctx context.Context, token string) (domain.Tokens, error) {
	now := s.d.Now()
	hash := domain.HashRefresh(token)
	sess, err := s.d.Sessions.FindByRefresh(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		prev, prevErr := s.d.Sessions.FindByPrevRefresh(ctx, hash)
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
		// A client that lost the answer to the refresh that revoked its
		// session for a suspension retries with the same token. Only that
		// token's holder learns of the suspension.
		if sess.RevokeReason() == domain.RevokedSuspended {
			a, loadErr := s.load(ctx, sess.AccountID())
			if loadErr != nil {
				return domain.Tokens{}, loadErr
			}
			if a.Suspended(now) {
				return domain.Tokens{}, domain.ErrSuspended
			}
		}
		return domain.Tokens{}, fmt.Errorf("refresh: %w", err)
	}
	a, err := s.load(ctx, sess.AccountID())
	if err != nil {
		return domain.Tokens{}, err
	}
	// A suspended account's session ends with account_suspended; a deleted
	// one's, which a sign-in racing Erase may have issued, with
	// session_revoked.
	st := a.Status(now)
	if r := st.Refusal(); r != nil {
		reason := domain.RevokedDeleted
		if st == domain.StatusSuspended {
			reason = domain.RevokedSuspended
		}
		if err = s.revoke(ctx, sess.ID(), reason, now); err != nil {
			return domain.Tokens{}, err
		}
		return domain.Tokens{}, r
	}
	next := sess.Rotate(now)
	if err = s.d.Sessions.Save(ctx, &sess, uuid.NewV7()); err != nil {
		if !errors.Is(err, repository.ErrConflict) {
			return domain.Tokens{}, fmt.Errorf("save session: %w", err)
		}
		// The write that won is a concurrent refresh with the same token, or a
		// sign-out. Only the first means two parties hold the token.
		cur, getErr := s.d.Sessions.Get(ctx, sess.ID())
		if getErr != nil {
			return domain.Tokens{}, fmt.Errorf("get session: %w", getErr)
		}
		if cur.RevokedAt() != nil {
			return domain.Tokens{}, domain.ErrSessionRevoked
		}
		return domain.Tokens{}, s.reused(ctx, sess.ID(), now, "concurrent refresh")
	}
	tokens, err := s.d.Signer.Tokens(sess, next, now)
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("issue tokens: %w", err)
	}
	return tokens, nil
}

// reused revokes a session whose refresh token two parties hold, records the
// security event and returns the refusal.
func (s *Account) reused(ctx context.Context, id uuid.UUID, now time.Time, why string) error {
	if err := s.revoke(ctx, id, domain.RevokedReuse, now); err != nil {
		return err
	}
	s.d.Log.WarnContext(ctx, "security: refresh token reuse, session revoked", "why", why, "session_id", id)
	refreshReuse.Add(ctx, 1)
	return domain.ErrSessionRevoked
}

// revoke reloads the session and revokes it for reason, retrying once when
// a concurrent write gets there first. A session revoked already keeps its
// reason.
func (s *Account) revoke(ctx context.Context, id uuid.UUID, reason domain.RevokeReason, now time.Time) error {
	var err error
	for range 2 {
		var sess domain.Session
		if sess, err = s.d.Sessions.Get(ctx, id); err != nil {
			return fmt.Errorf("get session: %w", err)
		}
		if sess.RevokedAt() != nil {
			return nil
		}
		sess.Revoke(reason, now)
		err = s.d.Sessions.Save(ctx, &sess, uuid.NewV7())
		if err == nil {
			return nil
		}
		if !errors.Is(err, repository.ErrConflict) {
			return fmt.Errorf("save session: %w", err)
		}
	}
	return fmt.Errorf("revoke session: %w", err)
}
