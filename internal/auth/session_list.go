package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type SessionItem struct {
	ID         uuid.UUID `json:"id"`
	DeviceName string    `json:"device_name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

// ListSessions lists the caller's devices, newest first.
func ListSessions(ctx context.Context, sessions session.Store, p authn.Principal, req page.Request, now time.Time) (page.Page[SessionItem], error) {
	rows, err := sessions.ListActive(ctx, p.AccountID, now, req)
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

// ListSessionsHandler serves GET /v1/auth/sessions.
func ListSessionsHandler(sessions session.Store, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		var in page.Request
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		out, err := ListSessions(r.Context(), sessions, p, in, now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, out)
	})
}
