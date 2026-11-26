package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/console/domain"
)

// EnrollTOTP gives a member without TOTP a fresh secret, which the first
// correct code at Login confirms; a second call replaces it. The password
// is checked and charged as in Login. An enrolled member is refused, so a
// stolen password cannot replace the authenticator; an admin resets it.
// It is UC-MOD-10 A2.
func (s *Console) EnrollTOTP(ctx context.Context, email, password string) (Enrollment, error) {
	now := s.d.Now()
	m, key, err := s.checkPassword(ctx, email, password, now)
	if err != nil {
		return Enrollment{}, err
	}
	if m.Next() == domain.StepChangePassword {
		return Enrollment{}, s.refund(ctx, key, now, domain.ErrPasswordChangeRequired)
	}
	// E5
	if m.Enrolled() {
		return Enrollment{}, s.refund(ctx, key, now, domain.ErrTOTPEnrolled)
	}
	secret := domain.NewTOTPSecret()
	m.StartEnrollment(secret, now)
	if err = s.d.Members.Save(ctx, &m, uuid.NewV7(), nil); err != nil {
		return Enrollment{}, saveFailed(err)
	}
	if err = s.refund(ctx, key, now, nil); err != nil {
		return Enrollment{}, err
	}
	return Enrollment{Secret: domain.EncodeSecret(secret), OTPAuthURI: domain.TOTPURI(secret, m.Email())}, nil
}
