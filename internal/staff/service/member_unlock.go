package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/domain"
)

// UnlockMember clears the failed sign-ins and the lock on a member's email.
func (s *Staff) UnlockMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	now := s.now()
	err = s.locks.Unlock(ctx, domain.LockKey(s.keys.Locks, m.Email()), now, audit.Entry{Actor: by, Action: audit.StaffUnlock, Target: audit.Staff(id), At: now})
	if err != nil {
		return MemberView{}, fmt.Errorf("unlock staff member: %w", err)
	}
	return viewOf(m, nil), nil
}
