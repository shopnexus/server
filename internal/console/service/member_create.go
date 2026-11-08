package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/platform/audit"
)

// CreateMember creates a member with a temporary password, records
// member.create for in.By, and returns the password once. The API creates
// moderators; cmd/console creates admins.
func (s *Console) CreateMember(ctx context.Context, in CreateMemberInput) (MemberView, string, error) {
	now := s.now()
	temp := domain.TemporaryPassword()
	m, err := domain.NewMember(uuid.NewV7(), in.Email, in.DisplayName, in.Role, s.keys.Hasher.Hash(temp), now)
	if err != nil {
		return MemberView{}, "", fmt.Errorf("new console member: %w", err)
	}
	err = s.members.CreateAudited(ctx, m, audit.Entry{
		Actor: in.By, Action: audit.MemberCreate, Target: audit.Member(m.ID()), Details: map[string]any{"role": in.Role}, At: now,
	})
	if err != nil {
		return MemberView{}, "", fmt.Errorf("create console member: %w", err)
	}
	return viewOf(m, nil), temp, nil
}
