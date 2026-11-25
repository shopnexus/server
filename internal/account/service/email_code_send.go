package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/contact"
)

// SendEmailCode mails a code to the address through the EmailCodeSend
// service. A retry of the Idempotency-Key answers with the challenge the
// first request made and mails nothing.
func (s *Account) SendEmailCode(ctx context.Context, in SendEmailCodeInput) (EmailCodeView, error) {
	addr, err := contact.ParseEmail(in.Email)
	if err != nil {
		return EmailCodeView{}, err //nolint:wrapcheck // contact.ErrInvalidEmail is the client's answer.
	}
	purpose, accountID := domain.PurposeSignIn, (*uuid.UUID)(nil)
	if in.Caller != nil {
		if err = s.mayAddIdentity(ctx, *in.Caller); err != nil {
			return EmailCodeView{}, err
		}
		purpose, accountID = domain.PurposeAddIdentity, &in.Caller.AccountID
	}
	c, err := s.challenges.ByKey(ctx, addr, in.Key)
	if err == nil {
		return EmailCodeView{ChallengeID: c.ID(), ExpiresAt: c.ExpiresAt(), ResendAt: c.CreatedAt().Add(resend.Window)}, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return EmailCodeView{}, fmt.Errorf("find the key's challenge: %w", err)
	}
	res, err := s.emailCodeSend.Call(ctx, EmailCodeInput{Email: addr, Purpose: purpose, AccountID: accountID, IP: in.IP, Locale: in.Locale, Key: in.Key})
	if err != nil {
		return EmailCodeView{}, err
	}
	return EmailCodeView{ChallengeID: res.ChallengeID, ExpiresAt: res.ExpiresAt, ResendAt: res.ResendAt}, nil
}
