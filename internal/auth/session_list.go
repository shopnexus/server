package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

const defaultPageSize = 20

var errBadPage = apperr.New(apperr.KindBadRequest, "invalid_request", "cursor or limit is not valid")

type SessionItem struct {
	ID         uuid.UUID `json:"id"`
	DeviceName string    `json:"device_name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

type SessionPage struct {
	Items      []SessionItem `json:"items"`
	NextCursor *uuid.UUID    `json:"next_cursor,omitempty"`
}

// ListSessions lists the caller's devices, newest first. A zero cursor starts
// at the newest; a zero limit means 20.
func ListSessions(ctx context.Context, sessions session.Store, p authn.Principal, cursor uuid.UUID, limit int, now time.Time) (SessionPage, error) {
	if limit == 0 {
		limit = defaultPageSize
	}
	rows, err := sessions.ListActive(ctx, p.AccountID, now, cursor, limit+1)
	if err != nil {
		return SessionPage{}, err
	}
	page := SessionPage{Items: []SessionItem{}}
	if len(rows) > limit {
		rows = rows[:limit]
		next := rows[limit-1].ID
		page.NextCursor = &next
	}
	for _, v := range rows {
		page.Items = append(page.Items, SessionItem{ID: v.ID, DeviceName: v.DeviceName, CreatedAt: v.CreatedAt,
			LastUsedAt: v.LastUsedAt, Current: v.ID == p.SessionID})
	}
	return page, nil
}

// GET /v1/auth/sessions
func ListSessionsHandler(sessions session.Store, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		var cursor uuid.UUID
		var limit int
		q := r.URL.Query()
		if c := q.Get("cursor"); c != "" {
			id, err := uuid.Parse(c)
			if err != nil {
				httpx.WriteError(w, r, log, errBadPage)
				return
			}
			cursor = id
		}
		if l := q.Get("limit"); l != "" {
			n, err := strconv.Atoi(l)
			if err != nil || n < 1 || n > 100 {
				httpx.WriteError(w, r, log, errBadPage)
				return
			}
			limit = n
		}
		page, err := ListSessions(r.Context(), sessions, p, cursor, limit, now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, page)
	})
}
