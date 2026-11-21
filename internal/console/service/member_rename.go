package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/platform/audit"
)

// RenameMember changes a moderator's display name; the audit row keeps both.
func (s *Console) RenameMember(ctx context.Context, id uuid.UUID, displayName string, by audit.Actor) (MemberView, error) {
	m, err := s.loadManaged(ctx, id, by)
	if err != nil {
		return MemberView{}, err
	}
	from, now := m.DisplayName(), s.now()
	if err = m.Rename(displayName, now); err != nil {
		return MemberView{}, err
	}
	return s.saveManaged(ctx, &m, by, audit.MemberRename, now, map[string]any{"from": from, "to": m.DisplayName()})
}
