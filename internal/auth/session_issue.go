package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

// IssueSession starts session id and signs its first access token. When the
// session exists already, because the client replayed an Idempotency-Key, it
// rotates the refresh token instead, so the replay also gets working tokens.
func IssueSession(ctx context.Context, sessions session.Store, signer session.Signer, id, accountID uuid.UUID, deviceName string, now time.Time) (session.Tokens, error) {
	s, refresh := session.New(id, accountID, deviceName, now, now)
	created, err := sessions.Create(ctx, s)
	if err != nil {
		return session.Tokens{}, fmt.Errorf("create session: %w", err)
	}
	if !created {
		if s, err = sessions.Get(ctx, id); err != nil {
			return session.Tokens{}, fmt.Errorf("get session: %w", err)
		}
		// A replay must not bring back a session that was signed out since.
		if err = s.Usable(now); err != nil {
			return session.Tokens{}, fmt.Errorf("replayed session: %w", err)
		}
		refresh = s.Rotate(now)
		if err = sessions.Save(ctx, &s, uuid.NewV7()); err != nil {
			if errors.Is(err, session.ErrConflict) {
				return session.Tokens{}, apperr.ErrTryAgain
			}
			return session.Tokens{}, fmt.Errorf("save session: %w", err)
		}
	}
	tokens, err := signer.Tokens(s, refresh, now)
	if err != nil {
		return session.Tokens{}, fmt.Errorf("issue tokens: %w", err)
	}
	return tokens, nil
}
