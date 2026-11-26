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
// A member already disabled (or, for EnableMember, active) writes nothing.
// It is UC-MOD-04 A1.
func (s *Console) DisableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error) {
	// E2 for an admin, E5 for no member.
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	now := s.d.Now()
	// A4
	if !m.Disable(now) {
		return viewOf(m), nil
	}
	return s.saveManaged(ctx, &m, by, audit.MemberDisable, now, nil)
}

// EnableMember lets a disabled member sign in again.
func (s *Console) EnableMember(ctx context.Context, id uuid.UUID, by audit.Actor) (MemberView, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	now := s.d.Now()
	if !m.Enable(now) {
		return viewOf(m), nil
	}
	return s.saveManaged(ctx, &m, by, audit.MemberEnable, now, nil)
}

// loadManaged reads the member an admin or the CLI acts on, refusing an
// admin target to anyone but the CLI.
func (s *Console) loadManaged(ctx context.Context, id uuid.UUID, by audit.Actor) (domain.Member, error) {
	m, err := s.d.Members.Get(ctx, id)
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

// saveManaged saves a changed member with an audit row of action and
// details, which may be nil.
func (s *Console) saveManaged(ctx context.Context, m *domain.Member, by audit.Actor, action audit.ActionKind, now time.Time, details map[string]any) (MemberView, error) {
	err := s.d.Members.Save(ctx, m, uuid.NewV7(), &audit.Entry{Actor: by, Action: action, Target: audit.Member(m.ID()), Details: details, At: now})
	if err != nil {
		return MemberView{}, saveFailed(err)
	}
	return viewOf(*m), nil
}
