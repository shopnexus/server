package staff

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/staff/quota"
)

// ListMembers lists members newest first, each with the end of its email's
// sign-in lock when one is in force. The locks of a page come in one query.
func ListMembers(ctx context.Context, st Stores, k Keys, p page.Request, now time.Time) (page.Page[MemberView], error) {
	rows, err := st.Members.List(ctx, p)
	if err != nil {
		return page.Page[MemberView]{}, fmt.Errorf("list staff members: %w", err)
	}
	rows, more := page.Cut(rows, p)
	keys := make([][]byte, len(rows))
	for i := range rows {
		keys[i] = quota.Key(k.Quota, rows[i].Email())
	}
	locks, err := st.Quotas.LockedUntil(ctx, keys, now)
	if err != nil {
		return page.Page[MemberView]{}, fmt.Errorf("list staff login locks: %w", err)
	}
	out := page.Page[MemberView]{Items: make([]MemberView, 0, len(rows))}
	if more {
		out.NextCursor = new(rows[len(rows)-1].ID())
	}
	for i := range rows {
		var lockedUntil *time.Time
		if until, ok := locks[string(keys[i])]; ok {
			lockedUntil = &until
		}
		out.Items = append(out.Items, viewOf(rows[i], lockedUntil))
	}
	return out, nil
}

// ListMembersHandler serves GET /v1/staff/members.
func ListMembersHandler(st Stores, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in page.Request
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		out, err := ListMembers(r.Context(), st, k, in, now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, out)
	})
}
