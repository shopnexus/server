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

// CreateMember creates a member with a temporary password, records
// staff.create for by, and returns the password once. The API creates
// moderators; cmd/staff creates admins.
func CreateMember(ctx context.Context, members member.Store, h Hasher, email, displayName string, role member.Role, by audit.Actor, now time.Time) (MemberView, string, error) {
	temp := member.TemporaryPassword()
	m, err := member.New(uuid.NewV7(), email, displayName, role, h.Hash(temp), now)
	if err != nil {
		return MemberView{}, "", fmt.Errorf("new staff member: %w", err)
	}
	v := viewOf(m, nil)
	err = members.CreateAudited(ctx, m, audit.Entry{
		Actor: by, Action: "staff.create", Target: audit.Staff(m.ID()), Diff: audit.Diff(nil, v), At: now,
	})
	if err != nil {
		return MemberView{}, "", fmt.Errorf("create staff member: %w", err)
	}
	return v, temp, nil
}

type CreateMemberRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// CreatedMember is the CreatedMember schema.
type CreatedMember struct {
	Member            MemberView `json:"member"`
	TemporaryPassword string     `json:"temporary_password"`
}

// CreateMemberHandler serves POST /v1/staff/members. The API creates
// moderators only.
func CreateMemberHandler(st Stores, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := httpx.StaffCaller(w, r, log)
		if !ok {
			return
		}
		var in CreateMemberRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		v, temp, err := CreateMember(r.Context(), st.Members, k.Hasher, in.Email, in.DisplayName, member.RoleModerator, audit.ByStaff(caller), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusCreated, CreatedMember{Member: v, TemporaryPassword: temp})
	})
}
