package auth

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/session"
)

// IssueSession starts session id and signs its first access token. When the
// session exists already, because the client replayed an Idempotency-Key, it
// rotates the refresh token instead, so the replay also gets working tokens.
func IssueSession(ctx context.Context, sessions session.Store, signer session.Signer, id, accountID uuid.UUID, deviceName string, now time.Time) (session.Tokens, error) {
	s, refresh := session.New(id, accountID, deviceName, now, now)
	created, err := sessions.Create(ctx, s)
	if err != nil {
		return session.Tokens{}, err
	}
	if !created {
		if s, err = sessions.Get(ctx, id); err != nil {
			return session.Tokens{}, err
		}
		// A replay must not bring back a session that was signed out since.
		if err := s.Usable(now); err != nil {
			return session.Tokens{}, err
		}
		refresh = s.Rotate(now)
		if err := sessions.Save(ctx, &s, uuid.NewV7()); err != nil {
			if errors.Is(err, session.ErrConflict) {
				return session.Tokens{}, ErrTryAgain
			}
			return session.Tokens{}, err
		}
	}
	return signer.Tokens(s, refresh, now)
}
