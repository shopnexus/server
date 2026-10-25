package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/staff/domain"
)

// ListMembers lists members newest first, each with the end of its email's
// sign-in lock when one is in force. The locks of a page come in one query.
func (s *Staff) ListMembers(ctx context.Context, p page.Request) (page.Page[MemberView], error) {
	rows, err := s.members.List(ctx, p)
	if err != nil {
		return page.Page[MemberView]{}, fmt.Errorf("list staff members: %w", err)
	}
	keys := make([][]byte, len(rows))
	for i := range rows {
		keys[i] = domain.LockKey(s.keys.Locks, rows[i].Email())
	}
	until, err := s.locks.LockedUntil(ctx, keys, s.now())
	if err != nil {
		return page.Page[MemberView]{}, fmt.Errorf("list staff login locks: %w", err)
	}
	views := make([]MemberView, len(rows))
	for i := range rows {
		var lockedUntil *time.Time
		if t, ok := until[string(keys[i])]; ok {
			lockedUntil = &t
		}
		views[i] = viewOf(rows[i], lockedUntil)
	}
	return page.Of(views, p, func(v MemberView) uuid.UUID { return v.ID }), nil
}
