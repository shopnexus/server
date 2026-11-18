package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/repository"
	"github.com/shopnexus/server/internal/platform/audit"
)

// DisableMember stops a member signing in and, by moving the credentials
// version, ends every session; enabling the member later brings none back.
func (s *Console) DisableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	now := s.now()
	m.Disable(now)
	return s.saveManaged(ctx, &m, by, audit.MemberDisable, now)
}

// EnableMember lets a disabled member sign in again.
func (s *Console) EnableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	now := s.now()
	m.Enable(now)
	return s.saveManaged(ctx, &m, by, audit.MemberEnable, now)
}

// loadManaged reads the member an admin or the CLI acts on, refusing an
// admin target to anyone but the CLI.
func (s *Console) loadManaged(ctx context.Context, id uuid.UUID, by audit.Actor) (domain.Member, error) {
	m, err := s.members.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Member{}, domain.ErrMemberNotFound
	}
	if err != nil {
		return domain.Member{}, fmt.Errorf("get console member: %w", err)
	}
	if err = m.ManagedBy(by); err != nil {
		return domain.Member{}, fmt.Errorf("manage console member: %w", err)
	}
	return m, nil
}

// saveManaged saves a changed member with an audit row of action.
func (s *Console) saveManaged(ctx context.Context, m *domain.Member, by audit.Actor, action audit.ActionKind, now time.Time) (MemberView, error) {
	err := s.members.Save(ctx, m, uuid.NewV7(), &audit.Entry{Actor: by, Action: action, Target: audit.Member(m.ID()), At: now})
	if err != nil {
		return MemberView{}, saveFailed(err)
	}
	return viewOf(*m, nil), nil
}
