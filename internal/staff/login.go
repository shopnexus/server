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

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/quota"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login checks an email and password and returns the next sign-in step. An
// unknown email, a wrong password and a disabled member fail alike, and an
// unknown email still costs one argon2 verification, so the response time
// does not tell them apart either. Every attempt is charged to the email's
// quota before the password is checked; a correct password takes back only
// its own charge, and only a finished sign-in clears the count.
func Login(ctx context.Context, members member.Store, quotas quota.Store, k Keys, email, password string, ip netip.Addr, now time.Time) (LoginStep, error) {
	email = member.NormalizeEmail(email)
	key := quota.Key(k.Quota, email)
	adm, err := quotas.Charge(ctx, key, now)
	if err != nil {
		return LoginStep{}, fmt.Errorf("charge login quota: %w", err)
	}
	if !adm.Admitted {
		return LoginStep{}, locked(adm.RetryAfter)
	}
	m, err := members.FindByEmail(ctx, email)
	if errors.Is(err, member.ErrNotFound) {
		member.VerifyPassword(k.dummy, password)
		return LoginStep{}, fail(ctx, quotas, key, adm.Charged, audit.Anonymous(ip), audit.Target{Kind: "", ID: uuid.UUID{}}, member.ErrInvalidCredentials, now)
	}
	if err != nil {
		return LoginStep{}, fmt.Errorf("find staff member: %w", err)
	}
	if !member.VerifyPassword(m.PasswordHash(), password) || m.Status() != member.StatusActive {
		return LoginStep{}, fail(ctx, quotas, key, adm.Charged, audit.Anonymous(ip), audit.Staff(m.ID()), member.ErrInvalidCredentials, now)
	}
	if err = quotas.Refund(ctx, key, now); err != nil {
		return LoginStep{}, fmt.Errorf("refund login quota: %w", err)
	}
	return issueTicket(k, &m, now)
}

// fail records a charged attempt that failed as staff.login_failed, or as
// staff.login_locked for the attempt that locks the email, and returns
// refusal.
func fail(ctx context.Context, quotas quota.Store, key []byte, charged int, by audit.Actor, target audit.Target, refusal error, now time.Time) error {
	action := "staff.login_failed"
	if charged >= quota.MaxFailures {
		action = "staff.login_locked"
	}
	if err := quotas.Fail(ctx, key, charged, now, audit.Entry{Actor: by, Action: action, Target: target, Diff: nil, At: now}); err != nil {
		return fmt.Errorf("record failed sign-in: %w", err)
	}
	return refusal
}

// LoginHandler serves POST /v1/staff/login.
func LoginHandler(members member.Store, quotas quota.Store, k Keys, trusted []netip.Prefix, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in LoginRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		step, err := Login(r.Context(), members, quotas, k, in.Email, in.Password, httpx.ClientIP(r, trusted), now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, step)
	})
}
