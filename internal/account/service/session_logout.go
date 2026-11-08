package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/authn"
)

// recentAuth is how long after a sign-in the sensitive actions stay open.
const recentAuth = 15 * time.Minute

// Logout revokes the caller's session. Its access token keeps working until
// it expires; the app discards it.
func (s *Account) Logout(ctx context.Context, p authn.Principal) error {
	return s.revoke(ctx, p.SessionID, s.now())
}

// LogoutAll revokes every session of the caller's account after a recent
// sign-in.
func (s *Account) LogoutAll(ctx context.Context, p authn.Principal) error {
	now := s.now()
	if now.Sub(p.AuthTime) > recentAuth {
		return domain.ErrReauthRequired
	}
	if err := s.sessions.RevokeAll(ctx, p.AccountID, now); err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	return nil
}

// RemoveSession signs one of the caller's devices out.
func (s *Account) RemoveSession(ctx context.Context, p authn.Principal, id uuid.UUID) error {
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
	return s.revoke(ctx, id, s.now())
}
