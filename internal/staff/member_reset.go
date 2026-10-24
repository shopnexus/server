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
	"github.com/shopnexus/server2/internal/staff/member"
)

// TemporaryPassword is the TemporaryPassword schema.
type TemporaryPassword struct {
	TemporaryPassword string `json:"temporary_password"`
}

// ResetMemberPassword gives a member a temporary password, which ends every
// session, and returns it once.
func ResetMemberPassword(ctx context.Context, st Stores, k Keys, id uuid.UUID, by audit.Actor, now time.Time) (string, error) {
	m, err := loadManaged(ctx, st.Members, id, by)
	if err != nil {
		return "", err
	}
	temp := member.TemporaryPassword()
	m.ResetPassword(k.Hasher.Hash(temp), now)
	err = st.Members.SaveAudited(ctx, &m, uuid.NewV7(), audit.Entry{
		Actor: by, Action: "staff.reset_password", Target: audit.Staff(id), Diff: map[string]any{"password": "reset"}, At: now,
	})
	if errors.Is(err, member.ErrConflict) {
		return "", apperr.ErrTryAgain
	}
	if err != nil {
		return "", fmt.Errorf("save staff member: %w", err)
	}
	return temp, nil
}

// ResetMemberTOTP removes a member's TOTP, which ends every session; the
// member enrolls again at the next sign-in.
func ResetMemberTOTP(ctx context.Context, st Stores, id uuid.UUID, by audit.Actor, now time.Time) (MemberView, error) {
	m, err := loadManaged(ctx, st.Members, id, by)
	if err != nil {
		return MemberView{}, err
	}
	before := viewOf(m, nil)
	m.ResetTOTP(now)
	return saveManaged(ctx, st.Members, &m, before, by, "staff.reset_totp", now)
}

// ResetMemberPasswordHandler serves POST /v1/staff/members/{id}/reset-password.
func ResetMemberPasswordHandler(st Stores, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
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
		temp, err := ResetMemberPassword(r.Context(), st, k, in.ID, audit.ByStaff(caller), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, TemporaryPassword{TemporaryPassword: temp})
	})
}

// ResetMemberTOTPHandler serves POST /v1/staff/members/{id}/reset-totp.
func ResetMemberTOTPHandler(st Stores, now func() time.Time, log *slog.Logger) http.Handler {
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
		v, err := ResetMemberTOTP(r.Context(), st, in.ID, audit.ByStaff(caller), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, v)
	})
}
