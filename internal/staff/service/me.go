package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/staff/domain"
)

// GetMe reads the signed-in member.
func (s *Staff) GetMe(ctx context.Context, caller authn.Staff) (Me, error) {
	m, err := s.members.Get(ctx, caller.StaffID)
	if err != nil {
		return Me{}, fmt.Errorf("get staff member: %w", err)
	}
	return meOf(m), nil
}

// ChangePassword replaces the caller's password. The credentials version
// moves, which ends every session of the member, the caller's included.
func (s *Staff) ChangePassword(ctx context.Context, caller authn.Staff, current, next string) error {
	m, err := s.members.Get(ctx, caller.StaffID)
	if err != nil {
		return fmt.Errorf("get staff member: %w", err)
	}
	if m.Status() != domain.StatusActive {
		return authn.ErrUnauthenticated
	}
	if !domain.VerifyPassword(m.PasswordHash(), current) {
		return domain.ErrInvalidCredentials
	}
	if err = m.CheckNewPassword(next); err != nil {
		return fmt.Errorf("new password: %w", err)
	}
	m.SetPassword(s.keys.Hasher.Hash(next), s.now())
	if err = s.members.Save(ctx, &m, uuid.NewV7()); err != nil {
		return saveFailed(err)
	}
	return nil
}

// Logout signs the caller's session out.
func (s *Staff) Logout(ctx context.Context, caller authn.Staff) error {
	if err := s.sessions.Revoke(ctx, caller.SessionID, s.now()); err != nil {
		return fmt.Errorf("sign out: %w", err)
	}
	return nil
}
