package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// SignIn checks the proof, has the SignIn service find or create the
// account, then starts its session. A retry of the Idempotency-Key gets the
// session the first request started.
func (s *Account) SignIn(ctx context.Context, in SignInInput) (SignedIn, error) {
	run := ProvenSignIn{Key: in.Key}
	if in.Proof.EmailCode != nil {
		g, err := s.guessFor(ctx, *in.Proof.EmailCode)
		if err != nil {
			return SignedIn{}, err
		}
		run.Guess = &g
	} else {
		addr, err := s.verifyToken(ctx, in.Proof, apperr.KindUnauthenticated)
		if err != nil {
			return SignedIn{}, err
		}
		run.Address = &addr
	}
	res, err := s.signIn.Call(ctx, "", run)
	if err != nil {
		return SignedIn{}, err
	}
	// The language is a preference: a sign-in that names none keeps it, and
	// one that cannot record it still signs in.
	if in.Locale != "" {
		if err = s.accounts.SetLocale(ctx, res.AccountID, string(in.Locale)); err != nil {
			s.log.WarnContext(ctx, "record the locale", "err", err)
		}
	}
	tokens, err := s.issueSession(ctx, uuid.NewV7(), res.AccountID, in.Key, in.DeviceName)
	if err != nil {
		return SignedIn{}, err
	}
	return SignedIn{Tokens: tokens, Account: SignedInAccount{ID: res.AccountID, IsNew: res.IsNew}}, nil
}
