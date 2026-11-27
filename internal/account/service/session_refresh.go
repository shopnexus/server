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
// session is revoked. A suspended account refreshes as any other; a deleted
// one's session is revoked.
//
// Refresh is UC-ACC-02 A3.
func (s *Account) Refresh(ctx context.Context, token string) (domain.Tokens, error) {
	now := s.d.Now()
	hash := domain.HashRefresh(token)
	sess, err := s.d.Sessions.FindByRefresh(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		prev, prevErr := s.d.Sessions.FindByPrevRefresh(ctx, hash)
		// E4
		if errors.Is(prevErr, repository.ErrNotFound) {
			return domain.Tokens{}, domain.ErrInvalidRefreshToken
		}
		if prevErr != nil {
			return domain.Tokens{}, fmt.Errorf("find session by previous token: %w", prevErr)
		}
		// E5 · NFR-SEC-01: a rotated token presented again.
		return domain.Tokens{}, s.reused(ctx, prev.ID(), now, "rotated token presented again")
	}
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("find session by token: %w", err)
	}
	// E5, E6: signed out or expired.
	if err = sess.Usable(now); err != nil {
		return domain.Tokens{}, fmt.Errorf("refresh: %w", err)
	}
	a, err := s.load(ctx, sess.AccountID())
	if err != nil {
		return domain.Tokens{}, err
	}
	// E5. A deleted account's session, which a sign-in racing Erase may have
	// issued, ends with session_revoked.
	if a.Status(now) == domain.StatusDeleted {
		if err = s.revoke(ctx, sess.ID(), domain.RevokedDeleted, now); err != nil {
			return domain.Tokens{}, err
		}
		return domain.Tokens{}, domain.ErrSessionRevoked
	}
	// A4 · BR-MOD-02: a suspended account keeps its session, to ask support.
	// A3. Rotate the refresh token.
	next := sess.Rotate(now)
	if err = s.d.Sessions.Save(ctx, &sess, uuid.NewV7()); err != nil {
		if !errors.Is(err, repository.ErrConflict) {
			return domain.Tokens{}, fmt.Errorf("save session: %w", err)
		}
		// E5. The write that won is a concurrent refresh with the same token, or a
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
