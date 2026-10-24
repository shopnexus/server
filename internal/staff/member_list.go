package staff

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/staff/member"
)

// ListMembers lists members newest first, each with the end of its email's
// sign-in lock when one is in force. The locks of a page come in one query.
func ListMembers(ctx context.Context, members member.Store, locks LockStore, k Keys, p page.Request, now time.Time) (page.Page[MemberView], error) {
	rows, err := members.List(ctx, p)
	if err != nil {
		return page.Page[MemberView]{}, fmt.Errorf("list staff members: %w", err)
	}
	keys := make([][]byte, len(rows))
	for i := range rows {
		keys[i] = lockKey(k.Locks, rows[i].Email())
	}
	until, err := locks.LockedUntil(ctx, keys, now)
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

// ListMembersHandler serves GET /v1/staff/members.
func ListMembersHandler(st Stores, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in page.Request
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		out, err := ListMembers(r.Context(), st.Members, st.Locks, k, in, now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, out)
	})
}
