package service

import (
	"context"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/contact"
)

// SendEmailCode mails a code to the address through the EmailCodeSend
// service. The invocation key holds the code's owner and the address, so a
// key sent again for another address or account runs apart.
func (s *Account) SendEmailCode(ctx context.Context, in SendEmailCodeInput) (EmailCodeView, error) {
	addr, err := contact.ParseEmail(in.Email)
	if err != nil {
		return EmailCodeView{}, err //nolint:wrapcheck // contact.ErrInvalidEmail is the client's answer.
	}
	purpose, owner := domain.PurposeSignIn, "-"
	if in.AccountID != nil {
		purpose, owner = domain.PurposeAddIdentity, in.AccountID.String()
	}
	res, err := s.emailCodeSend.Call(ctx, owner+"/"+addr.Value()+"/"+in.IdempotencyKey,
		EmailCodeInput{Email: addr, Purpose: purpose, AccountID: in.AccountID, IP: in.IP})
	if err != nil {
		return EmailCodeView{}, err
	}
	return EmailCodeView{ChallengeID: res.ChallengeID, ExpiresAt: res.ExpiresAt, ResendAt: res.ResendAt}, nil
}
