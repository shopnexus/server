package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/authn"
)

// DeleteCurrentSession revokes the caller's session. Its access token keeps working until
// it expires; the app discards it.
//
// DeleteCurrentSession is UC-ACC-02, steps 2 and 3.
func (s *Account) DeleteCurrentSession(ctx context.Context, p authn.Principal) error {
	return s.revoke(ctx, p.SessionID, domain.RevokedSignOut, s.d.Now())
}

// DeleteOtherSessions signs out every session of the caller's account but
// the caller's own, after a recent sign-in.
//
// DeleteOtherSessions is UC-ACC-02 A1.
func (s *Account) DeleteOtherSessions(ctx context.Context, p authn.Principal) error {
	now := s.d.Now()
	// E2 · BR-ACC-04
	if now.Sub(p.AuthTime) > domain.RecentAuth {
		return domain.ErrReauthRequired
	}
	if err := s.d.Sessions.RevokeOthers(ctx, p.AccountID, p.SessionID, now, uuid.NewV7()); err != nil {
		return fmt.Errorf("revoke the other sessions: %w", err)
	}
	return nil
}

// DeleteSession signs one of the caller's devices out.
//
// DeleteSession is UC-ACC-02, steps 2 and 3.
func (s *Account) DeleteSession(ctx context.Context, p authn.Principal, id uuid.UUID) error {
	// E1 · BR-ACC-02: another account's session is not found either.
	sess, err := s.d.Sessions.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if sess.AccountID() != p.AccountID {
		return domain.ErrSessionNotFound
	}
	return s.revoke(ctx, id, domain.RevokedSignOut, s.d.Now())
}
