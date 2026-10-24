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
	"github.com/shopnexus/server2/internal/staff/quota"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login checks an email and password and returns the next sign-in step. An
// unknown email, a wrong password and a disabled member fail alike, and an
// unknown email still costs one argon2 verification, so the response time
// does not tell them apart either. A correct password does not clear the
// failure count; only a finished sign-in does.
func Login(ctx context.Context, members member.Store, quotas quota.Store, k Keys, email, password string, ip netip.Addr, now time.Time) (LoginStep, error) {
	email = member.NormalizeEmail(email)
	key := quota.Key(k.Quota, email)
	q, err := quotas.Get(ctx, key)
	if err != nil {
		return LoginStep{}, fmt.Errorf("get login quota: %w", err)
	}
	if wait, isLocked := q.Locked(now); isLocked {
		return LoginStep{}, locked(wait)
	}
	m, err := members.FindByEmail(ctx, email)
	if errors.Is(err, member.ErrNotFound) {
		member.VerifyPassword(k.dummy, password)
		return LoginStep{}, fail(ctx, quotas, key, audit.Anonymous(ip), audit.Target{Kind: "", ID: uuid.UUID{}}, member.ErrInvalidCredentials, now)
	}
	if err != nil {
		return LoginStep{}, fmt.Errorf("find staff member: %w", err)
	}
	if !member.VerifyPassword(m.PasswordHash(), password) || m.Status() != member.StatusActive {
		return LoginStep{}, fail(ctx, quotas, key, audit.Anonymous(ip), audit.Staff(m.ID()), member.ErrInvalidCredentials, now)
	}
	return issueTicket(k, &m, now)
}

// fail counts a failed sign-in on key, records staff.login_failed, or
// staff.login_locked for the failure that locks, and returns refusal. A save
// that loses a race reads the quota again, so concurrent failures all count.
func fail(ctx context.Context, quotas quota.Store, key []byte, by audit.Actor, target audit.Target, refusal error, now time.Time) error {
	for range 5 {
		q, err := quotas.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("get login quota: %w", err)
		}
		action := "staff.login_failed"
		if q.Fail(now) {
			action = "staff.login_locked"
		}
		err = quotas.SaveAudited(ctx, &q, uuid.NewV7(), audit.Entry{Actor: by, Action: action, Target: target, Diff: nil, At: now})
		if err == nil {
			return refusal
		}
		if !errors.Is(err, quota.ErrConflict) {
			return fmt.Errorf("save login quota: %w", err)
		}
	}
	return apperr.ErrTryAgain
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
