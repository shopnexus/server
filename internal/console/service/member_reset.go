package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/audit"
)

// ResetMemberPassword gives a member a temporary password, which ends every
// session, and returns it once.
func (s *Console) ResetMemberPassword(ctx context.Context, id uuid.UUID, by audit.Actor) (string, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return "", err
	}
	now := s.now()
	temp := domain.TemporaryPassword()
	m.ResetPassword(s.keys.Hasher.Hash(temp), now)
	if _, err = s.saveManaged(ctx, &m, by, audit.MemberResetPassword, now, nil); err != nil {
		return "", err
	}
	return temp, nil
}

// ResetMemberTOTP removes a member's TOTP, which ends every session; the
// member enrolls again at the next sign-in.
func (s *Console) ResetMemberTOTP(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	now := s.now()
	m.ResetTOTP(now)
	return s.saveManaged(ctx, &m, by, audit.MemberResetTOTP, now, nil)
}
