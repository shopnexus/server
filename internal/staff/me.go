package staff

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/member"
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
// moves, which ends every session of the member, the caller's included.
func ChangePassword(ctx context.Context, members member.Store, k Keys, caller authn.Staff, current, next string, now time.Time) error {
	m, err := members.Get(ctx, caller.StaffID)
	if err != nil {
		return fmt.Errorf("get staff member: %w", err)
	}
	if m.Status() != member.StatusActive {
		return authn.ErrUnauthenticated
	}
	if !member.VerifyPassword(m.PasswordHash(), current) {
		return member.ErrInvalidCredentials
	}
	if err = m.CheckNewPassword(next); err != nil {
		return fmt.Errorf("new password: %w", err)
	}
	m.SetPassword(k.Hasher.Hash(next), now)
	if err = members.Save(ctx, &m, uuid.NewV7()); err != nil {
		return saveFailed(err)
	}
	return nil
}

// Logout signs the caller's session out.
func Logout(ctx context.Context, sessions SessionStore, caller authn.Staff, now time.Time) error {
	if err := sessions.Revoke(ctx, caller.SessionID, now); err != nil {
		return fmt.Errorf("sign out: %w", err)
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

// ChangePasswordHandler serves PUT /v1/staff/me/password and clears the
// cookie of the session the change ended.
func ChangePasswordHandler(members member.Store, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
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
		if err := ChangePassword(r.Context(), members, k, caller, in.CurrentPassword, in.NewPassword, now()); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		clearSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	})
}

// LogoutHandler serves POST /v1/staff/logout and clears the cookie.
func LogoutHandler(sessions SessionStore, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := httpx.StaffCaller(w, r, log)
		if !ok {
			return
		}
		if err := Logout(r.Context(), sessions, caller, now()); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		clearSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	})
}
