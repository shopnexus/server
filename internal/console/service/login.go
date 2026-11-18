package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/token"
)

const maxUserAgent = 500

// Login signs a member in. Every call sends the email and password; the
// member decides what else the call needs. A temporary password needs
// NewPassword, which replaces it. A member without TOTP enrolls through
// EnrollTOTP and confirms with the first code; an enrolled member sends a
// code. A missing piece is a 428 that names it. With SkipTOTP the password
// (and a new one when due) is enough, and the code is never asked for.
//
// The attempt is charged to the email's lock before anything is checked. A
// wrong password or code keeps the charge; any other answer takes it back,
// and a finished sign-in clears the count.
func (s *Console) Login(ctx context.Context, in LoginInput) (SignedIn, error) {
	now := s.now()
	m, key, err := s.checkPassword(ctx, in.Email, in.Password, now)
	if err != nil {
		return SignedIn{}, err
	}
	if m.Next() == domain.StepChangePassword {
		if in.NewPassword == "" {
			return SignedIn{}, s.refund(ctx, key, now, domain.ErrPasswordChangeRequired)
		}
		if policyErr := m.CheckNewPassword(in.NewPassword); policyErr != nil {
			return SignedIn{}, s.refund(ctx, key, now, policyErr)
		}
		m.SetPassword(s.keys.Hasher.Hash(in.NewPassword), now)
		if err = s.members.Save(ctx, &m, uuid.NewV7(), nil); err != nil {
			return SignedIn{}, saveFailed(err)
		}
	}
	if !s.skipTOTP {
		if err = s.acceptCode(ctx, &m, key, in.Code, now); err != nil {
			return SignedIn{}, err
		}
	}
	if err = s.locks.Clear(ctx, key, now); err != nil {
		return SignedIn{}, fmt.Errorf("clear login lock: %w", err)
	}
	if r := []rune(in.UserAgent); len(r) > maxUserAgent {
		in.UserAgent = string(r[:maxUserAgent])
	}
	tok, hash := token.NewOpaque("ss_")
	sess := domain.Session{
		ID: uuid.NewV7(), MemberID: m.ID(), TokenHash: hash, CredentialsVersion: m.CredentialsVersion(),
		IP: in.IP, UserAgent: in.UserAgent, CreatedAt: now, ExpiresAt: now.Add(domain.SessionTTL),
	}
	if err = s.sessions.Create(ctx, sess); err != nil {
		return SignedIn{}, fmt.Errorf("create console session: %w", err)
	}
	return SignedIn{Me: meOf(m), Token: tok}, nil
}

// acceptCode checks the member's TOTP code and saves the accepted step,
// which is what stops the code working twice.
func (s *Console) acceptCode(ctx context.Context, m *domain.Member, key []byte, code string, now time.Time) error {
	switch {
	case m.Next() == domain.StepEnrollTOTP && !m.Enrolling():
		return s.refund(ctx, key, now, domain.ErrTOTPEnrollmentRequired)
	case code == "":
		return s.refund(ctx, key, now, domain.ErrTOTPRequired)
	}
	if m.AcceptCode(code, now) != nil {
		if err := s.locks.Fail(ctx, key, now); err != nil {
			return fmt.Errorf("record failed sign-in: %w", err)
		}
		return domain.ErrInvalidTOTPCode
	}
	if err := s.members.Save(ctx, m, uuid.NewV7(), nil); err != nil {
		return saveFailed(err)
	}
	return nil
}

// checkPassword charges a sign-in attempt to the email's lock, then checks
// the password. An unknown email, a wrong password and a disabled member
// fail alike and keep the charge; an unknown email still costs one argon2
// run, so the response time does not tell them apart either. On success the
// charge is still counted, and the caller refunds or keeps it.
func (s *Console) checkPassword(ctx context.Context, email, password string, now time.Time) (domain.Member, []byte, error) {
	email = contact.NormalizeEmail(email)
	key := domain.LockKey(s.keys.Locks, email)
	if err := s.locks.Charge(ctx, key, now); err != nil {
		return domain.Member{}, nil, fmt.Errorf("charge login lock: %w", err)
	}
	m, err := s.members.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return domain.Member{}, nil, fmt.Errorf("find console member: %w", err)
	}
	hash := s.keys.dummy
	if err == nil {
		hash = m.PasswordHash()
	}
	if !domain.VerifyPassword(hash, password) || err != nil || m.Status() != domain.StatusActive {
		if failErr := s.locks.Fail(ctx, key, now); failErr != nil {
			return domain.Member{}, nil, fmt.Errorf("record failed sign-in: %w", failErr)
		}
		return domain.Member{}, nil, domain.ErrInvalidCredentials
	}
	return m, key, nil
}

// refund takes back the charge of an attempt whose password was right and
// returns refusal.
func (s *Console) refund(ctx context.Context, key []byte, now time.Time, refusal error) error {
	if err := s.locks.Refund(ctx, key, now); err != nil {
		return fmt.Errorf("refund login lock: %w", err)
	}
	return refusal
}

// saveFailed is the answer to a failed member save.
func saveFailed(err error) error {
	if errors.Is(err, repository.ErrConflict) {
		return apperr.ErrTryAgain
	}
	return fmt.Errorf("save console member: %w", err)
}
