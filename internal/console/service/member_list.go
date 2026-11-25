package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// ListMembers lists members newest first, each with the end of its email's
// sign-in lock when one is in force and its last sign-in. The locks and the
// sign-ins of a page come in one query each.
func (s *Console) ListMembers(ctx context.Context, p page.Request) (page.Page[MemberView], error) {
	rows, err := s.d.Members.List(ctx, p)
	if err != nil {
		return page.Page[MemberView]{}, fmt.Errorf("list console members: %w", err)
	}
	keys := make([][]byte, len(rows))
	ids := make([]uuid.UUID, len(rows))
	for i := range rows {
		keys[i] = domain.LockKey(s.d.Keys.Locks, rows[i].Email())
		ids[i] = rows[i].ID()
	}
	until, err := s.d.Locks.LockedUntil(ctx, keys, s.d.Now())
	if err != nil {
		return page.Page[MemberView]{}, fmt.Errorf("list console login locks: %w", err)
	}
	last, err := s.d.Sessions.LastSignIns(ctx, ids)
	if err != nil {
		return page.Page[MemberView]{}, fmt.Errorf("list console last sign-ins: %w", err)
	}
	views := make([]MemberView, len(rows))
	for i := range rows {
		views[i] = viewOf(rows[i])
		if t, ok := until[string(keys[i])]; ok {
			views[i].LockedUntil = &t
		}
		if t, ok := last[rows[i].ID()]; ok {
			views[i].LastSignInAt = &t
		}
	}
	return page.Of(views, p, func(v MemberView) uuid.UUID { return v.ID }), nil
}
