package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/domain"
)

// ResetMemberPassword gives a member a temporary password, which ends every
// session, and returns it once.
func (s *Staff) ResetMemberPassword(ctx context.Context, id uuid.UUID, by audit.Actor) (string, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return "", err
	}
	now := s.now()
	temp := domain.TemporaryPassword()
	m.ResetPassword(s.keys.Hasher.Hash(temp), now)
	if _, err = s.saveManaged(ctx, &m, by, audit.StaffResetPassword, now); err != nil {
		return "", err
	}
	return temp, nil
}

// ResetMemberTOTP removes a member's TOTP, which ends every session; the
// member enrolls again at the next sign-in.
func (s *Staff) ResetMemberTOTP(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	now := s.now()
	m.ResetTOTP(now)
	return s.saveManaged(ctx, &m, by, audit.StaffResetTOTP, now)
}
