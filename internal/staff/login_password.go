package staff

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/member"
)

type LoginPasswordRequest struct {
	Ticket      string `json:"ticket"`
	NewPassword string `json:"new_password"`
}

// ChangeTemporaryPassword replaces the temporary password of the member a
// ticket names and returns a ticket at the new credentials version.
func ChangeTemporaryPassword(ctx context.Context, members member.Store, k Keys, ticket, newPassword string, ip netip.Addr, now time.Time) (LoginStep, error) {
	m, err := readTicket(ctx, members, k, ticket, now)
	if err != nil {
		return LoginStep{}, err
	}
	if m.Next() != member.StepChangePassword {
		return LoginStep{}, ErrInvalidLoginTicket
	}
	if policyErr := member.CheckPolicy(newPassword, m.Email()); policyErr != nil {
		return LoginStep{}, fmt.Errorf("new password: %w", policyErr)
	}
	if member.VerifyPassword(m.PasswordHash(), newPassword) {
		return LoginStep{}, member.ErrPasswordTooWeak
	}
	m.SetPassword(k.Hasher.Hash(newPassword), now)
	err = members.SaveAudited(ctx, &m, uuid.NewV7(), audit.Entry{
		Actor: audit.BySignIn(m.ID(), ip), Action: "staff.password_change", Target: audit.Staff(m.ID()),
		Diff: map[string]any{"password": "changed"}, At: now,
	})
	if errors.Is(err, member.ErrConflict) {
		return LoginStep{}, apperr.ErrTryAgain
	}
	if err != nil {
		return LoginStep{}, fmt.Errorf("save staff member: %w", err)
	}
	return issueTicket(k, &m, now)
}

// ChangeTemporaryPasswordHandler serves POST /v1/staff/login/password.
func ChangeTemporaryPasswordHandler(members member.Store, k Keys, trusted []netip.Prefix, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in LoginPasswordRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		step, err := ChangeTemporaryPassword(r.Context(), members, k, in.Ticket, in.NewPassword, httpx.ClientIP(r, trusted), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, step)
	})
}
