package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/domain"
)

// CreateMember creates a member with a temporary password, records
// staff.create for in.By, and returns the password once. The API creates
// moderators; cmd/staff creates admins.
func (s *Staff) CreateMember(ctx context.Context, in CreateMemberInput) (MemberView, string, error) {
	now := s.now()
	temp := domain.TemporaryPassword()
	m, err := domain.NewMember(uuid.NewV7(), in.Email, in.DisplayName, in.Role, s.keys.Hasher.Hash(temp), now)
	if err != nil {
		return MemberView{}, "", fmt.Errorf("new staff member: %w", err)
	}
	err = s.members.CreateAudited(ctx, m, audit.Entry{
		Actor: in.By, Action: "staff.create", Target: audit.Staff(m.ID()), Details: map[string]any{"role": in.Role}, At: now,
	})
	if err != nil {
		return MemberView{}, "", fmt.Errorf("create staff member: %w", err)
	}
	return viewOf(m, nil), temp, nil
}
