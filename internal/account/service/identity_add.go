package service

import (
	"context"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
)

// AddIdentity checks the caller and the proof, then has the AddIdentity
// service add the address to the caller's account. A retry of the
// Idempotency-Key answers with the account's identities.
//
// AddIdentity is UC-ACC-08, steps 1 and 2.
func (s *Account) AddIdentity(ctx context.Context, in AddIdentityInput) (IdentitiesView, error) {
	if err := s.mayAddIdentity(ctx, in.Caller); err != nil {
		return IdentitiesView{}, err
	}
	run := AddIdentityRun{AccountID: in.Caller.AccountID, Key: in.Key}
	// A1. An add-email code is judged in the flow.
	if in.Proof.EmailCode != nil {
		g, err := s.guessFor(ctx, *in.Proof.EmailCode)
		if err != nil {
			return IdentitiesView{}, err
		}
		run.Guess = &g
	} else {
		// 2. Check the phone or Google token as a sign-in does.
		addr, err := s.verifyToken(ctx, in.Proof, apperr.KindInvalid)
		if err != nil {
			return IdentitiesView{}, err
		}
		run.Address = &addr
	}
	res, err := s.addIdentity.Call(ctx, run)
	if err != nil {
		return IdentitiesView{}, err
	}
	return IdentitiesView{Identities: res.Identities}, nil
}

// mayAddIdentity refuses an account that may not write, then a sign-in
// older than domain.RecentAuth: a sign-in method added with a leaked access
// token would let its holder in for good, since none is ever removed.
// AddIdentity and SendEmailCode call it. It is UC-ACC-08 E1 and E2.
func (s *Account) mayAddIdentity(ctx context.Context, p authn.Principal) error {
	a, err := s.load(ctx, p.AccountID)
	if err != nil {
		return err
	}
	now := s.d.Now()
	// E2
	if err = a.CanWrite(now); err != nil {
		return err
	}
	// E1 · BR-ACC-04
	if now.Sub(p.AuthTime) > domain.RecentAuth {
		return domain.ErrReauthRequired
	}
	return nil
}
