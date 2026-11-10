package service

import (
	"context"

	"github.com/shopnexus/server2/internal/account/domain"
)

// SignIn checks the proof, has the SignIn service find or create the
// account, then starts the session it named. The invocation key holds the
// proven address, or the challenge of an email code, so a key sent again
// with another person's proof runs apart.
func (s *Account) SignIn(ctx context.Context, in SignInInput) (SignedIn, error) {
	var (
		run       ProvenSignIn
		key       string
		codeMatch = true
	)
	if in.Proof.EmailCode != nil {
		g, match, err := s.guessFor(ctx, *in.Proof.EmailCode)
		if err != nil {
			return SignedIn{}, err
		}
		run.Guess, key, codeMatch = &g, g.ChallengeID.String()+"/"+in.IdempotencyKey, match
	} else {
		addr, err := s.verifyToken(ctx, in.Proof)
		if err != nil {
			return SignedIn{}, err
		}
		run.Address, key = &addr, string(addr.Kind())+":"+addr.Value()+"/"+in.IdempotencyKey
	}
	res, err := s.signIn.Call(ctx, key, run)
	if err != nil {
		return SignedIn{}, err
	}
	// A repeated key returns the first run's result; only the right code
	// gets its session.
	if !codeMatch {
		return SignedIn{}, domain.ErrCodeExpired
	}
	tokens, err := s.issueSession(ctx, res.SessionID, res.AccountID, in.DeviceName)
	if err != nil {
		return SignedIn{}, err
	}
	return SignedIn{Tokens: tokens, Account: SignedInAccount{ID: res.AccountID, IsNew: res.IsNew}}, nil
}
