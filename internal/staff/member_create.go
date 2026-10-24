package staff

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/member"
)

// CreateMember creates a member with a temporary password, records
// staff.create for by, and returns the password once. The API creates
// moderators; cmd/staff creates admins.
func CreateMember(ctx context.Context, members member.Store, h Hasher, email, displayName string, role member.Role, by audit.Actor, now time.Time) (MemberView, string, error) {
	temp := member.TemporaryPassword()
	m, err := member.New(uuid.NewV7(), email, displayName, role, h.Hash(temp), now)
	if err != nil {
		return MemberView{}, "", fmt.Errorf("new staff member: %w", err)
	}
	v := viewOf(m, nil)
	err = members.CreateAudited(ctx, m, audit.Entry{
		Actor: by, Action: "staff.create", Target: audit.Staff(m.ID()), Diff: audit.Diff(nil, v), At: now,
	})
	if err != nil {
		return MemberView{}, "", fmt.Errorf("create staff member: %w", err)
	}
	return v, temp, nil
}
