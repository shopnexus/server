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
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/session"
)

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// GetMe reads the signed-in member.
func GetMe(ctx context.Context, members member.Store, caller authn.Staff) (Me, error) {
	m, err := members.Get(ctx, caller.StaffID)
	if err != nil {
		return Me{}, fmt.Errorf("get staff member: %w", err)
	}
	return meOf(m), nil
}

// ChangePassword replaces the caller's password. The credentials version
// moves, which ends the member's other sessions; the caller's own session
// moves to the new version and stays.
func ChangePassword(ctx context.Context, st Stores, k Keys, caller authn.Staff, current, next string, now time.Time) error {
	m, err := st.Members.Get(ctx, caller.StaffID)
	if err != nil {
		return fmt.Errorf("get staff member: %w", err)
	}
	if !member.VerifyPassword(m.PasswordHash(), current) {
		return member.ErrInvalidCredentials
	}
	if policyErr := member.CheckPolicy(next, m.Email()); policyErr != nil {
		return fmt.Errorf("new password: %w", policyErr)
	}
	if member.VerifyPassword(m.PasswordHash(), next) {
		return member.ErrPasswordTooWeak
	}
	m.SetPassword(k.Hasher.Hash(next), now)
	err = st.Members.SaveAudited(ctx, &m, uuid.NewV7(), audit.Entry{
		Actor: audit.ByStaff(caller), Action: "staff.password_change", Target: audit.Staff(m.ID()),
		Diff: map[string]any{"password": "changed"}, At: now,
	})
	if errors.Is(err, member.ErrConflict) {
		return apperr.ErrTryAgain
	}
	if err != nil {
		return fmt.Errorf("save staff member: %w", err)
	}
	for range 2 {
		var s session.Session
		if s, err = st.Sessions.Get(ctx, caller.SessionID); err != nil {
			return fmt.Errorf("get staff session: %w", err)
		}
		s.SetVersion(m.CredentialsVersion())
		if err = st.Sessions.Save(ctx, &s, uuid.NewV7()); !errors.Is(err, session.ErrConflict) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("move staff session to the new password: %w", err)
	}
	return nil
}

// GetMeHandler serves GET /v1/staff/me.
func GetMeHandler(members member.Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := httpx.StaffCaller(w, r, log)
		if !ok {
			return
		}
		me, err := GetMe(r.Context(), members, caller)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, me)
	})
}

// ChangePasswordHandler serves PUT /v1/staff/me/password.
func ChangePasswordHandler(st Stores, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := httpx.StaffCaller(w, r, log)
		if !ok {
			return
		}
		var in ChangePasswordRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		if err := ChangePassword(r.Context(), st, k, caller, in.CurrentPassword, in.NewPassword, now()); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
