package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/contact"
)

// SendEmailCode mails a code to the address through the EmailCodeSend
// service. The invocation key holds the code's owner and the address, so a
// key sent again for another address or account runs apart.
func (s *Account) SendEmailCode(ctx context.Context, in SendEmailCodeInput) (EmailCodeView, error) {
	addr, err := contact.ParseEmail(in.Email)
	if err != nil {
		return EmailCodeView{}, err //nolint:wrapcheck // contact.ErrInvalidEmail is the client's answer.
	}
	purpose, owner, accountID := domain.PurposeSignIn, "-", (*uuid.UUID)(nil)
	if in.Caller != nil {
		if err = s.mayAddIdentity(ctx, *in.Caller); err != nil {
			return EmailCodeView{}, err
		}
		purpose, owner, accountID = domain.PurposeAddIdentity, in.Caller.AccountID.String(), &in.Caller.AccountID
	}
	res, err := s.emailCodeSend.Call(ctx, owner+"/"+addr.Value()+"/"+in.IdempotencyKey,
		EmailCodeInput{Email: addr, Purpose: purpose, AccountID: accountID, IP: in.IP, Locale: in.Locale})
	if err != nil {
		return EmailCodeView{}, err
	}
	return EmailCodeView{ChallengeID: res.ChallengeID, ExpiresAt: res.ExpiresAt, ResendAt: res.ResendAt}, nil
}
