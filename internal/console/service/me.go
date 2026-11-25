package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/authn"
)

// GetMe reads the signed-in member, with the ops dashboards' floors.
func (s *Console) GetMe(ctx context.Context, caller authn.ConsoleMember) (Me, error) {
	m, err := s.d.Members.Get(ctx, caller.MemberID)
	if err != nil {
		return Me{}, fmt.Errorf("get console member: %w", err)
	}
	me := meOf(m)
	if me.Dashboards, err = s.DashboardFloors(ctx); err != nil {
		return Me{}, err
	}
	return me, nil
}

// ChangePassword replaces the caller's password. The credentials version
// moves, which ends every session of the member, the caller's included.
func (s *Console) ChangePassword(ctx context.Context, caller authn.ConsoleMember, current, next string) error {
	m, err := s.d.Members.Get(ctx, caller.MemberID)
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
	m.SetPassword(s.d.Keys.Hasher.Hash(next), s.d.Now())
	if err = s.d.Members.Save(ctx, &m, uuid.NewV7(), nil); err != nil {
		return saveFailed(err)
	}
	return nil
}

// Logout signs the caller's session out.
func (s *Console) Logout(ctx context.Context, caller authn.ConsoleMember) error {
	if err := s.d.Sessions.Revoke(ctx, caller.SessionID, s.d.Now()); err != nil {
		return fmt.Errorf("sign out: %w", err)
	}
	return nil
}
