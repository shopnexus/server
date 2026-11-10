package service

import (
	"context"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/authn"
)

// AddIdentity checks the caller and the proof, then has the AddIdentity
// service add the address to the caller's account. The key holds the
// account, so another account's key runs apart; a repeated key answers the
// owner with their own identities, so no code check follows the call.
func (s *Account) AddIdentity(ctx context.Context, in AddIdentityInput) (IdentitiesView, error) {
	if err := s.mayAddIdentity(ctx, in.Caller); err != nil {
		return IdentitiesView{}, err
	}
	run := AddIdentityRun{AccountID: in.Caller.AccountID}
	if in.Proof.EmailCode != nil {
		g, _, err := s.guessFor(ctx, *in.Proof.EmailCode)
		if err != nil {
			return IdentitiesView{}, err
		}
		run.Guess = &g
	} else {
		addr, err := s.verifyToken(ctx, in.Proof)
		if err != nil {
			return IdentitiesView{}, err
		}
		run.Address = &addr
	}
	res, err := s.addIdentity.Call(ctx, in.Caller.AccountID.String()+"/"+in.IdempotencyKey, run)
	if err != nil {
		return IdentitiesView{}, err
	}
	return IdentitiesView{Identities: res.Identities}, nil
}

// mayAddIdentity refuses an account that may not write, then a sign-in
// older than domain.RecentAuth: a sign-in method added with a leaked access
// token would let its holder in for good, since none is ever removed.
// AddIdentity and SendEmailCode call it.
func (s *Account) mayAddIdentity(ctx context.Context, p authn.Principal) error {
	a, err := s.load(ctx, p.AccountID)
	if err != nil {
		return err
	}
	now := s.now()
	if err = a.CanWrite(now); err != nil {
		return err
	}
	if now.Sub(p.AuthTime) > domain.RecentAuth {
		return domain.ErrReauthRequired
	}
	return nil
}
