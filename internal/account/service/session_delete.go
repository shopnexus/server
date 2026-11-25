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
func (s *Account) DeleteCurrentSession(ctx context.Context, p authn.Principal) error {
	return s.revoke(ctx, p.SessionID, domain.RevokedSignOut, s.now())
}

// DeleteOtherSessions signs out every session of the caller's account but
// the caller's own, after a recent sign-in.
func (s *Account) DeleteOtherSessions(ctx context.Context, p authn.Principal) error {
	now := s.now()
	if now.Sub(p.AuthTime) > domain.RecentAuth {
		return domain.ErrReauthRequired
	}
	if err := s.sessions.RevokeOthers(ctx, p.AccountID, p.SessionID, now, uuid.NewV7()); err != nil {
		return fmt.Errorf("revoke the other sessions: %w", err)
	}
	return nil
}

// DeleteSession signs one of the caller's devices out.
func (s *Account) DeleteSession(ctx context.Context, p authn.Principal, id uuid.UUID) error {
	sess, err := s.sessions.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if sess.AccountID() != p.AccountID {
		return domain.ErrSessionNotFound
	}
	return s.revoke(ctx, id, domain.RevokedSignOut, s.now())
}
