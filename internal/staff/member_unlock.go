package staff

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/member"
)

// UnlockMember clears the failed sign-ins and the lock on a member's email.
func UnlockMember(ctx context.Context, members member.Store, locks LockStore, k Keys, id uuid.UUID, by audit.Actor, now time.Time) (MemberView, error) {
	m, err := loadManaged(ctx, members, id, by)
	if err != nil {
		return MemberView{}, err
	}
	err = locks.Unlock(ctx, lockKey(k.Locks, m.Email()), now, audit.Entry{Actor: by, Action: "staff.unlock", Target: audit.Staff(id), At: now})
	if err != nil {
		return MemberView{}, fmt.Errorf("unlock staff member: %w", err)
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
		v, err := UnlockMember(r.Context(), st.Members, st.Locks, k, in.ID, audit.ByStaff(caller), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, v)
	})
}
