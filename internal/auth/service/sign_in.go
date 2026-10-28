package service

import (
	"context"
	"fmt"
)

// SignIn checks the phone token, has the SignIn service find or create the
// account, then starts the session it named.
func (a *Auth) SignIn(ctx context.Context, in SignInInput) (SignedIn, error) {
	phone, err := a.phones.Verify(ctx, in.PhoneToken)
	if err != nil {
		return SignedIn{}, fmt.Errorf("verify phone token: %w", err)
	}
	// Restate answers a repeated key with the first run's result. The number
	// in the key sends another number's token to a run of its own.
	res, err := a.signIn.Call(ctx, phone.Value()+"/"+in.IdempotencyKey, PhoneSignIn{Phone: phone})
	if err != nil {
		return SignedIn{}, err
	}
	tokens, err := a.IssueSession(ctx, res.SessionID, res.AccountID, in.DeviceName)
	if err != nil {
		return SignedIn{}, err
	}
	return SignedIn{Tokens: tokens, Account: SignedInAccount{ID: res.AccountID, IsNew: res.IsNew}}, nil
}
