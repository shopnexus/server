package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/repository"
)

// DisableMember stops a member signing in and, by moving the credentials
// version, ends every session; enabling the member later brings none back.
func (s *Staff) DisableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	now := s.now()
	m.Disable(now)
	return s.saveManaged(ctx, &m, by, "staff.disable", now)
}

// EnableMember lets a disabled member sign in again.
func (s *Staff) EnableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	now := s.now()
	m.Enable(now)
	return s.saveManaged(ctx, &m, by, "staff.enable", now)
}

// loadManaged reads the member an admin or the CLI acts on, refusing an
// admin target to anyone but the CLI.
func (s *Staff) loadManaged(ctx context.Context, id uuid.UUID, by audit.Actor) (domain.Member, error) {
	m, err := s.members.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Member{}, domain.ErrMemberNotFound
	}
	if err != nil {
		return domain.Member{}, fmt.Errorf("get staff member: %w", err)
	}
	if err = m.ManagedBy(by); err != nil {
		return domain.Member{}, fmt.Errorf("manage staff member: %w", err)
	}
	return m, nil
}

// saveManaged saves a changed member with an audit row of action.
func (s *Staff) saveManaged(ctx context.Context, m *domain.Member, by audit.Actor, action string, now time.Time) (MemberView, error) {
	err := s.members.SaveAudited(ctx, m, uuid.NewV7(), audit.Entry{Actor: by, Action: action, Target: audit.Staff(m.ID()), At: now})
	if err != nil {
		return MemberView{}, saveFailed(err)
	}
	return viewOf(*m, nil), nil
}
