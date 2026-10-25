package service

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
)

// SignIn verifies a code through the SignIn service, which finds or creates
// the account, then starts the session it named.
func (a *Auth) SignIn(ctx context.Context, in SignInInput) (SignedIn, error) {
	c, err := a.challenges.Get(ctx, in.ChallengeID)
	if errors.Is(err, repository.ErrNotFound) {
		return SignedIn{}, domain.ErrCodeExpired
	}
	if err != nil {
		return SignedIn{}, fmt.Errorf("get challenge: %w", err)
	}
	hash := a.codes.Hash(c.Destination(), in.Code)
	res, err := a.signIn.Call(ctx, in.IdempotencyKey, VerifyInput{ChallengeID: in.ChallengeID, CodeHash: hash})
	if err != nil {
		return SignedIn{}, err
	}
	// Restate answers a repeated Idempotency-Key with the first result,
	// whatever this request carries. Only the request that matches it, the
	// same challenge and its code, gets the session.
	if res.ChallengeID != in.ChallengeID || !hmac.Equal(hash, c.CodeHash()) {
		return SignedIn{}, domain.ErrCodeExpired
	}
	tokens, err := a.IssueSession(ctx, res.SessionID, res.AccountID, in.DeviceName)
	if err != nil {
		return SignedIn{}, err
	}
	return SignedIn{Tokens: tokens, Account: SignedInAccount{ID: res.AccountID, IsNew: res.IsNew}}, nil
}
