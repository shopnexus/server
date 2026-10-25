package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/page"
)

// ListSessions lists the caller's devices, newest first.
func (a *Auth) ListSessions(ctx context.Context, p authn.Principal, req page.Request) (page.Page[SessionItem], error) {
	rows, err := a.sessions.ListActive(ctx, p.AccountID, a.now(), req)
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
	return page.Of(items, req, func(s SessionItem) uuid.UUID { return s.ID }), nil
}
