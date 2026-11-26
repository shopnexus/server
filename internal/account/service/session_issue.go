package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// issueSession starts session id of the account and signs its first access
// token. When the account has the session of key already, because the client
// sent the sign-in again, it rotates that session's refresh token instead,
// so the retry also gets working tokens. SignIn calls it after the SignIn
// service.
//
// issueSession is UC-ACC-01, step 4.
func (s *Account) issueSession(ctx context.Context, id, accountID, key uuid.UUID, deviceName string) (domain.Tokens, error) {
	now := s.d.Now()
	sess, refresh := domain.NewSession(id, accountID, deviceName, now, now)
	stored, err := s.d.Sessions.Create(ctx, sess, key)
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("create session: %w", err)
	}
	// A3 · NFR-IDEM-01
	if stored != sess.ID() {
		if sess, err = s.d.Sessions.Get(ctx, stored); err != nil {
			return domain.Tokens{}, fmt.Errorf("get session: %w", err)
		}
		// E11. A retry must not bring back a session that was signed out since.
		if err = sess.Usable(now); err != nil {
			return domain.Tokens{}, fmt.Errorf("retried session: %w", err)
		}
		refresh = sess.Rotate(now)
		if err = s.d.Sessions.Save(ctx, &sess, uuid.NewV7()); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return domain.Tokens{}, apperr.ErrTryAgain
			}
			return domain.Tokens{}, fmt.Errorf("save session: %w", err)
		}
	}
	tokens, err := s.d.Signer.Tokens(sess, refresh, now)
	if err != nil {
		return domain.Tokens{}, fmt.Errorf("issue tokens: %w", err)
	}
	return tokens, nil
}
