package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

// IssueSession starts session id and signs its first access token. When the
// session exists already, because the client replayed an Idempotency-Key, it
// rotates the refresh token instead, so the replay also gets working tokens.
// SignIn calls it after the SignIn service.
func (a *Auth) IssueSession(ctx context.Context, id, accountID uuid.UUID, deviceName string) (domain.Tokens, error) {
	now := a.now()
	s, refresh := domain.NewSession(id, accountID, deviceName, now, now)
	created, err := a.sessions.Create(ctx, s)
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("create session: %w", err)
	}
	if !created {
		if s, err = a.sessions.Get(ctx, id); err != nil {
			return domain.Tokens{}, fmt.Errorf("get session: %w", err)
		}
		// A replay must not bring back a session that was signed out since.
		if err = s.Usable(now); err != nil {
			return domain.Tokens{}, fmt.Errorf("replayed session: %w", err)
		}
		refresh = s.Rotate(now)
		if err = a.sessions.Save(ctx, &s, uuid.NewV7()); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return domain.Tokens{}, apperr.ErrTryAgain
			}
			return domain.Tokens{}, fmt.Errorf("save session: %w", err)
		}
	}
	tokens, err := a.signer.Tokens(s, refresh, now)
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("issue tokens: %w", err)
	}
	return tokens, nil
}
