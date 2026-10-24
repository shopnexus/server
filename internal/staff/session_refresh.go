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
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/session"
)

type RefreshRequest struct {
	Refresh string `cookie:"staff_refresh" json:"-"`
}

// Refreshed is a new access token and the rotated refresh token.
type Refreshed struct {
	Access Access
	cookie issued
}

// RefreshToken is the token the new cookie carries; tests read it.
func (r Refreshed) RefreshToken() string { return r.cookie.refresh }

// Refresh rotates the refresh token and signs a new access token. A rotated
// token presented again, or two refreshes racing with one token, means two
// parties hold it, so the session ends and staff.refresh_reuse is recorded.
// A disabled member or a moved credentials version ends the session too.
func Refresh(ctx context.Context, st Stores, k Keys, refresh string, ip netip.Addr, now time.Time) (Refreshed, error) {
	hash := token.HashRefresh(refresh)
	s, err := st.Sessions.FindByRefresh(ctx, hash)
	if errors.Is(err, session.ErrNotFound) {
		prev, prevErr := st.Sessions.FindByPrevRefresh(ctx, hash)
		if errors.Is(prevErr, session.ErrNotFound) {
			return Refreshed{}, session.ErrInvalidRefresh
		}
		if prevErr != nil {
			return Refreshed{}, fmt.Errorf("find staff session by previous token: %w", prevErr)
		}
		return Refreshed{}, reused(ctx, st.Sessions, prev.ID(), prev.StaffID(), ip, now)
	}
	if err != nil {
		return Refreshed{}, fmt.Errorf("find staff session by token: %w", err)
	}
	if err = s.Usable(now); err != nil {
		return Refreshed{}, fmt.Errorf("refresh: %w", err)
	}
	m, err := st.Members.Get(ctx, s.StaffID())
	if err != nil {
		return Refreshed{}, fmt.Errorf("get staff member: %w", err)
	}
	if m.Status() != member.StatusActive || m.CredentialsVersion() != s.CredentialsVersion() {
		if err = revoke(ctx, st.Sessions, s.ID(), nil, now); err != nil {
			return Refreshed{}, err
		}
		return Refreshed{}, session.ErrRevoked
	}
	next := s.Rotate(now)
	if err = st.Sessions.Save(ctx, &s, uuid.NewV7()); err != nil {
		if !errors.Is(err, session.ErrConflict) {
			return Refreshed{}, fmt.Errorf("save staff session: %w", err)
		}
		// The write that won is a refresh with the same token, or a sign-out.
		// Only the first means two parties hold the token.
		cur, getErr := st.Sessions.Get(ctx, s.ID())
		if getErr != nil {
			return Refreshed{}, fmt.Errorf("get staff session: %w", getErr)
		}
		if cur.RevokedAt() != nil {
			return Refreshed{}, session.ErrRevoked
		}
		return Refreshed{}, reused(ctx, st.Sessions, s.ID(), s.StaffID(), ip, now)
	}
	access, exp, err := k.Signer.Sign(token.Claims{
		Subject: m.ID(), SessionID: s.ID(), AuthTime: s.CreatedAt(), Version: 0, Audience: AudienceStaff, ExpiresAt: time.Time{},
	}, session.AccessTTL, now)
	if err != nil {
		return Refreshed{}, fmt.Errorf("sign staff access token: %w", err)
	}
	return Refreshed{
		Access: Access{AccessToken: access, AccessTokenExpiresAt: exp, TokenType: "Bearer"},
		cookie: issued{refresh: next, expiresAt: s.ExpiresAt()},
	}, nil
}

// reused ends a session whose refresh token two parties hold. Whoever
// presented the token proved nothing, so the record's actor is anonymous.
func reused(ctx context.Context, sessions session.Store, id, staffID uuid.UUID, ip netip.Addr, now time.Time) error {
	e := audit.Entry{Actor: audit.Anonymous(ip), Action: "staff.refresh_reuse", Target: audit.Staff(staffID), Diff: nil, At: now}
	if err := revoke(ctx, sessions, id, &e, now); err != nil {
		return err
	}
	return session.ErrRevoked
}

// revoke reloads the session and revokes it, recording e when given, and
// tries once more when a concurrent write gets there first.
func revoke(ctx context.Context, sessions session.Store, id uuid.UUID, e *audit.Entry, now time.Time) error {
	var err error
	for range 2 {
		var s session.Session
		if s, err = sessions.Get(ctx, id); err != nil {
			return fmt.Errorf("get staff session: %w", err)
		}
		if s.RevokedAt() != nil {
			return nil
		}
		s.Revoke(now)
		if e != nil {
			err = sessions.SaveAudited(ctx, &s, uuid.NewV7(), *e)
		} else {
			err = sessions.Save(ctx, &s, uuid.NewV7())
		}
		if err == nil {
			return nil
		}
		if !errors.Is(err, session.ErrConflict) {
			return fmt.Errorf("save staff session: %w", err)
		}
	}
	return fmt.Errorf("revoke staff session: %w", err)
}

// RefreshHandler serves POST /v1/staff/session/refresh. The spec requires
// X-Requested-With, which a cross-site form cannot set. A refusal clears the
// cookie, so the console stops retrying a dead session.
func RefreshHandler(st Stores, k Keys, trusted []netip.Prefix, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in RefreshRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		t := now()
		out, err := Refresh(r.Context(), st, k, in.Refresh, httpx.ClientIP(r, trusted), t)
		if errors.Is(err, session.ErrInvalidRefresh) || errors.Is(err, session.ErrRevoked) || errors.Is(err, session.ErrExpired) {
			clearRefreshCookie(w)
		}
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		setRefreshCookie(w, out.cookie, t)
		httpx.Respond(w, http.StatusOK, out.Access)
	})
}
