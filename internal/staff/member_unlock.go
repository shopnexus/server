package staff

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/quota"
)

// UnlockMember clears the failed sign-ins and the lock on a member's email.
func UnlockMember(ctx context.Context, st Stores, k Keys, id uuid.UUID, by audit.Actor, now time.Time) (MemberView, error) {
	m, err := loadManaged(ctx, st.Members, id, by)
	if err != nil {
		return MemberView{}, err
	}
	q, err := st.Quotas.Get(ctx, quota.Key(k.Quota, m.Email()))
	if err != nil {
		return MemberView{}, fmt.Errorf("get login quota: %w", err)
	}
	q.Clear(now)
	err = st.Quotas.SaveAudited(ctx, &q, uuid.NewV7(), audit.Entry{
		Actor: by, Action: "staff.unlock", Target: audit.Staff(id), Diff: nil, At: now,
	})
	if errors.Is(err, quota.ErrConflict) {
		return MemberView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return MemberView{}, fmt.Errorf("save login quota: %w", err)
	}
	return viewOf(m, nil), nil
}

// UnlockMemberHandler serves POST /v1/staff/members/{id}/unlock.
func UnlockMemberHandler(st Stores, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := httpx.StaffCaller(w, r, log)
		if !ok {
			return
		}
		var in MemberRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		v, err := UnlockMember(r.Context(), st, k, in.ID, audit.ByStaff(caller), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, v)
	})
}
