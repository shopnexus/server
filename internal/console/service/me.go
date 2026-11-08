package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/platform/authn"
)

// GetMe reads the signed-in member.
func (s *Console) GetMe(ctx context.Context, caller authn.ConsoleMember) (Me, error) {
	m, err := s.members.Get(ctx, caller.MemberID)
	if err != nil {
		return Me{}, fmt.Errorf("get console member: %w", err)
	}
	return meOf(m), nil
}

// ChangePassword replaces the caller's password. The credentials version
// moves, which ends every session of the member, the caller's included.
func (s *Console) ChangePassword(ctx context.Context, caller authn.ConsoleMember, current, next string) error {
	m, err := s.members.Get(ctx, caller.MemberID)
	if err != nil {
		return fmt.Errorf("get console member: %w", err)
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
	if err = s.members.Save(ctx, &m, uuid.NewV7(), nil); err != nil {
		return saveFailed(err)
	}
	return nil
}

// Logout signs the caller's session out.
func (s *Console) Logout(ctx context.Context, caller authn.ConsoleMember) error {
	if err := s.sessions.Revoke(ctx, caller.SessionID, s.now()); err != nil {
		return fmt.Errorf("sign out: %w", err)
	}
	return nil
}
