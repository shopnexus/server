package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/page"
)

// ListSessions lists the caller's devices, newest first.
//
// ListSessions is UC-ACC-02, step 1.
func (s *Account) ListSessions(ctx context.Context, p authn.Principal, req page.Request) (page.Page[SessionItem], error) {
	rows, err := s.d.Sessions.ListActive(ctx, p.AccountID, s.d.Now(), req)
	if err != nil {
		return page.Page[SessionItem]{}, fmt.Errorf("list sessions: %w", err)
	}
	items := make([]SessionItem, 0, len(rows))
	for _, v := range rows {
		items = append(items, SessionItem{
			ID: v.ID, DeviceName: v.DeviceName, CreatedAt: v.CreatedAt,
			LastUsedAt: v.LastUsedAt, Current: v.ID == p.SessionID,
		})
	}
	return page.Of(items, req, func(it SessionItem) uuid.UUID { return it.ID }), nil
}
