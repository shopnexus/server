package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// Reauthenticate renews the sign-in time of the caller's session after a
// new proof of the caller's own account. The middleware checks a recent
// sign-in from the access token's auth_time alone, so the caller needs a
// new access token, and the session row keeps the time for the tokens its
// refreshes sign. The refresh token is not rotated: an app that lost the
// reply would still hold the old one, and a refresh token presented after
// its rotation signs the session out.
func (s *Account) Reauthenticate(ctx context.Context, in ReauthenticateInput) (domain.AccessToken, error) {
	now := s.now()
	a, err := s.load(ctx, in.Caller.AccountID)
	if err != nil {
		return domain.AccessToken{}, err
	}
	if err = a.CanWrite(now); err != nil {
		return domain.AccessToken{}, err
	}
	sess, err := s.sessions.Get(ctx, in.Caller.SessionID)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.AccessToken{}, domain.ErrSessionRevoked
	}
	if err != nil {
		return domain.AccessToken{}, fmt.Errorf("get session: %w", err)
	}
	if err = sess.Usable(now); err != nil {
		return domain.AccessToken{}, err
	}

	run := ReauthenticateRun{AccountID: in.Caller.AccountID, Key: in.Key}
	if in.Proof.EmailCode != nil {
		g, guessErr := s.guessFor(ctx, *in.Proof.EmailCode)
		if guessErr != nil {
			return domain.AccessToken{}, guessErr
		}
		run.Guess = &g
	} else {
		addr, verifyErr := s.verifyToken(ctx, in.Proof, apperr.KindInvalid)
		if verifyErr != nil {
			return domain.AccessToken{}, verifyErr
		}
		run.Address = &addr
	}
	res, err := s.reauthenticate.Call(ctx, "", run)
	if err != nil {
		return domain.AccessToken{}, err
	}

	// auth_time only moves forward, so a retry of the request is harmless.
	// The session must still be usable at res.At, the time the proof was
	// checked, which is later than now.
	renewed, err := s.sessions.Reauthenticate(ctx, sess.ID(), res.At)
	if errors.Is(err, repository.ErrNotFound) {
		// It expired or went idle by res.At, or was signed out since the
		// check above.
		if err = sess.Usable(res.At); err != nil {
			return domain.AccessToken{}, err
		}
		return domain.AccessToken{}, domain.ErrSessionRevoked
	}
	if err != nil {
		return domain.AccessToken{}, fmt.Errorf("renew the sign-in time: %w", err)
	}
	access, exp, err := s.signer.Sign(renewed, now)
	if err != nil {
		return domain.AccessToken{}, fmt.Errorf("issue access token: %w", err)
	}
	return domain.AccessToken{AccessToken: access, AccessTokenExpiresAt: exp, TokenType: "Bearer"}, nil
}
