package staff

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/member"
)

// MemberRequest names a member by the path.
type MemberRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// DisableMember stops a member signing in and, by moving the credentials
// version, ends every session; enabling the member later brings none back.
func DisableMember(ctx context.Context, members member.Store, id uuid.UUID, by audit.Actor, now time.Time) (MemberView, error) {
	m, err := loadManaged(ctx, members, id, by)
	if err != nil {
		return MemberView{}, err
	}
	m.Disable(now)
	return saveManaged(ctx, members, &m, by, "staff.disable", now)
}

// EnableMember lets a disabled member sign in again.
func EnableMember(ctx context.Context, members member.Store, id uuid.UUID, by audit.Actor, now time.Time) (MemberView, error) {
	m, err := loadManaged(ctx, members, id, by)
	if err != nil {
		return MemberView{}, err
	}
	m.Enable(now)
	return saveManaged(ctx, members, &m, by, "staff.enable", now)
}

// loadManaged reads the member an admin or the CLI acts on, refusing an
// admin target to anyone but the CLI.
func loadManaged(ctx context.Context, members member.Store, id uuid.UUID, by audit.Actor) (member.Member, error) {
	m, err := members.Get(ctx, id)
	if errors.Is(err, member.ErrNotFound) {
		return member.Member{}, ErrMemberNotFound
	}
	if err != nil {
		return member.Member{}, fmt.Errorf("get staff member: %w", err)
	}
	if err = m.ManagedBy(by); err != nil {
		return member.Member{}, fmt.Errorf("manage staff member: %w", err)
	}
	return m, nil
}

// saveManaged saves a changed member with an audit row of action.
func saveManaged(ctx context.Context, members member.Store, m *member.Member, by audit.Actor, action string, now time.Time) (MemberView, error) {
	err := members.SaveAudited(ctx, m, uuid.NewV7(), audit.Entry{Actor: by, Action: action, Target: audit.Staff(m.ID()), At: now})
	if err != nil {
		return MemberView{}, saveFailed(err)
	}
	return viewOf(*m, nil), nil
}

// DisableMemberHandler serves POST /v1/staff/members/{id}/disable.
func DisableMemberHandler(members member.Store, now func() time.Time, log *slog.Logger) http.Handler {
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
		v, err := DisableMember(r.Context(), members, in.ID, audit.ByStaff(caller), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, v)
	})
}

// EnableMemberHandler serves POST /v1/staff/members/{id}/enable.
func EnableMemberHandler(members member.Store, now func() time.Time, log *slog.Logger) http.Handler {
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
		v, err := EnableMember(r.Context(), members, in.ID, audit.ByStaff(caller), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, v)
	})
}
