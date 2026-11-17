package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
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

	// The key holds the session, so another caller's key runs apart, and
	// the proven address or the code's challenge, as SignIn's does.
	var (
		run       = ReauthenticateRun{AccountID: in.Caller.AccountID}
		key       string
		codeMatch = true
	)
	if in.Proof.EmailCode != nil {
		g, match, guessErr := s.guessFor(ctx, *in.Proof.EmailCode)
		if guessErr != nil {
			return domain.AccessToken{}, guessErr
		}
		run.Guess, key, codeMatch = &g, sess.ID().String()+"/"+g.ChallengeID.String()+"/"+in.IdempotencyKey, match
	} else {
		addr, verifyErr := s.verifyToken(ctx, in.Proof)
		if verifyErr != nil {
			return domain.AccessToken{}, verifyErr
		}
		run.Address, key = &addr, sess.ID().String()+"/"+string(addr.Kind())+":"+addr.Value()+"/"+in.IdempotencyKey
	}
	res, err := s.reauthenticate.Call(ctx, key, run)
	if err != nil {
		return domain.AccessToken{}, err
	}
	// A repeated key returns the first run's result; only the right code
	// renews the session.
	if !codeMatch {
		return domain.AccessToken{}, domain.ErrCodeExpired
	}

	// A repeated key sets the first run's time again, which never moves
	// auth_time back. The session must still be usable at res.At, the time
	// the proof was checked; on a first run that is later than now.
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
