package service

import "context"

// AddIdentity checks the proof and has the AddIdentity service add the
// address to the caller's account. The key holds the account, so another
// account's key runs apart; a repeated key answers the owner with their own
// identities, so no code check follows the call.
func (s *Account) AddIdentity(ctx context.Context, in AddIdentityInput) (IdentitiesView, error) {
	run := AddIdentityRun{AccountID: in.AccountID}
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
	res, err := s.addIdentity.Call(ctx, in.AccountID.String()+"/"+in.IdempotencyKey, run)
	if err != nil {
		return IdentitiesView{}, err
	}
	return IdentitiesView{Identities: res.Identities}, nil
}
