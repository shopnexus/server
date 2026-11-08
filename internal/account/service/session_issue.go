package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

// IssueSession starts session id and signs its first access token. When the
// session exists already, because the client replayed an Idempotency-Key, it
// rotates the refresh token instead, so the replay also gets working tokens.
// SignIn calls it after the SignIn service.
func (s *Account) IssueSession(ctx context.Context, id, accountID uuid.UUID, deviceName string) (domain.Tokens, error) {
	now := s.now()
	sess, refresh := domain.NewSession(id, accountID, deviceName, now, now)
	created, err := s.sessions.Create(ctx, sess)
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("create session: %w", err)
	}
	if !created {
		if sess, err = s.sessions.Get(ctx, id); err != nil {
			return domain.Tokens{}, fmt.Errorf("get session: %w", err)
		}
		// A replay must not bring back a session that was signed out since.
		if err = sess.Usable(now); err != nil {
			return domain.Tokens{}, fmt.Errorf("replayed session: %w", err)
		}
		refresh = sess.Rotate(now)
		if err = s.sessions.Save(ctx, &sess, uuid.NewV7()); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return domain.Tokens{}, apperr.ErrTryAgain
			}
			return domain.Tokens{}, fmt.Errorf("save session: %w", err)
		}
	}
	tokens, err := s.signer.Tokens(sess, refresh, now)
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("issue tokens: %w", err)
	}
	return tokens, nil
}
