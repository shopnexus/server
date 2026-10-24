package staff

import (
	"context"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/member"
)

// TemporaryPassword is the TemporaryPassword schema.
type TemporaryPassword struct {
	TemporaryPassword string `json:"temporary_password"`
}

// ResetMemberPassword gives a member a temporary password, which ends every
// session, and returns it once.
func ResetMemberPassword(ctx context.Context, members member.Store, h member.Hasher, id uuid.UUID, by audit.Actor, now time.Time) (string, error) {
	m, err := loadManaged(ctx, members, id, by)
	if err != nil {
		return "", err
	}
	temp := member.TemporaryPassword()
	m.ResetPassword(h.Hash(temp), now)
	if _, err = saveManaged(ctx, members, &m, by, "staff.reset_password", now); err != nil {
		return "", err
	}
	return temp, nil
}

// ResetMemberTOTP removes a member's TOTP, which ends every session; the
// member enrolls again at the next sign-in.
func ResetMemberTOTP(ctx context.Context, members member.Store, id uuid.UUID, by audit.Actor, now time.Time) (MemberView, error) {
	m, err := loadManaged(ctx, members, id, by)
	if err != nil {
		return MemberView{}, err
	}
	m.ResetTOTP(now)
	return saveManaged(ctx, members, &m, by, "staff.reset_totp", now)
}

// ResetMemberPasswordHandler serves POST /v1/staff/members/{id}/reset-password.
func ResetMemberPasswordHandler(members member.Store, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
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
		temp, err := ResetMemberPassword(r.Context(), members, k.Hasher, in.ID, audit.ByStaff(caller), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, TemporaryPassword{TemporaryPassword: temp})
	})
}

// ResetMemberTOTPHandler serves POST /v1/staff/members/{id}/reset-totp.
func ResetMemberTOTPHandler(members member.Store, now func() time.Time, log *slog.Logger) http.Handler {
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
		v, err := ResetMemberTOTP(r.Context(), members, in.ID, audit.ByStaff(caller), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, v)
	})
}
