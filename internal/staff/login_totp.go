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
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/quota"
	"github.com/shopnexus/server2/internal/staff/session"
)

type TicketRequest struct {
	Ticket string `json:"ticket"`
}

type LoginTOTPRequest struct {
	Ticket    string `json:"ticket"`
	Code      string `json:"code"`
	UserAgent string `header:"User-Agent" json:"-"`
}

// EnrollTOTP gives the member a ticket names a fresh secret to enroll; a
// second call replaces it.
func EnrollTOTP(ctx context.Context, members member.Store, k Keys, ticket string, now time.Time) (Enrollment, error) {
	m, err := readTicket(ctx, members, k, ticket, now)
	if err != nil {
		return Enrollment{}, err
	}
	if m.Next() != member.StepEnrollTOTP {
		return Enrollment{}, ErrInvalidLoginTicket
	}
	secret := member.NewTOTPSecret()
	m.StartEnrollment(secret, now)
	err = members.Save(ctx, &m, uuid.NewV7())
	if errors.Is(err, member.ErrConflict) {
		return Enrollment{}, apperr.ErrTryAgain
	}
	if err != nil {
		return Enrollment{}, fmt.Errorf("save staff member: %w", err)
	}
	return Enrollment{Secret: member.EncodeSecret(secret), OTPAuthURI: member.TOTPURI(secret, m.Email())}, nil
}

// SignedIn is what a finished sign-in returns: the response body and the
// refresh token for the cookie.
type SignedIn struct {
	SignIn SignIn
	cookie issued
}

// RefreshToken is the token the cookie carries; tests read it.
func (s SignedIn) RefreshToken() string { return s.cookie.refresh }

// VerifyTOTP finishes sign-in with a code. A wrong code counts toward the
// email's lock; a right one confirms a pending enrollment, clears the
// failures and starts a session recorded as staff.login.
func VerifyTOTP(ctx context.Context, st Stores, k Keys, ticket, code string, ip netip.Addr, userAgent string, now time.Time) (SignedIn, error) {
	m, err := readTicket(ctx, st.Members, k, ticket, now)
	if err != nil {
		return SignedIn{}, err
	}
	if step := m.Next(); step == member.StepChangePassword || (step == member.StepEnrollTOTP && !m.Enrolling()) {
		return SignedIn{}, ErrInvalidLoginTicket
	}
	key := quota.Key(k.Quota, m.Email())
	adm, err := st.Quotas.Charge(ctx, key, now)
	if err != nil {
		return SignedIn{}, fmt.Errorf("charge login quota: %w", err)
	}
	if !adm.Admitted {
		return SignedIn{}, locked(adm.RetryAfter)
	}
	enrolling := !m.Enrolled()
	if codeErr := m.AcceptCode(code, now); codeErr != nil {
		return SignedIn{}, fail(ctx, st.Quotas, key, adm.Charged, audit.BySignIn(m.ID(), ip), audit.Staff(m.ID()), member.ErrInvalidTOTPCode, now)
	}
	// Saving the accepted step is what stops the code working twice.
	if enrolling {
		err = st.Members.SaveAudited(ctx, &m, uuid.NewV7(), audit.Entry{
			Actor: audit.BySignIn(m.ID(), ip), Action: "staff.totp_enroll", Target: audit.Staff(m.ID()),
			Diff: map[string]any{"totp": "enrolled"}, At: now,
		})
	} else {
		err = st.Members.Save(ctx, &m, uuid.NewV7())
	}
	if errors.Is(err, member.ErrConflict) {
		return SignedIn{}, apperr.ErrTryAgain
	}
	if err != nil {
		return SignedIn{}, fmt.Errorf("save staff member: %w", err)
	}
	if err = st.Quotas.Clear(ctx, key, now); err != nil {
		return SignedIn{}, fmt.Errorf("clear login quota: %w", err)
	}
	s, refresh := session.New(uuid.NewV7(), m.ID(), m.CredentialsVersion(), ip, userAgent, now)
	err = st.Sessions.CreateAudited(ctx, s, audit.Entry{
		Actor:  audit.Actor{Kind: audit.ActorStaff, StaffID: m.ID(), SessionID: s.ID(), Name: "", IP: ip},
		Action: "staff.login", Target: audit.Staff(m.ID()), Diff: nil, At: now,
	})
	if err != nil {
		return SignedIn{}, fmt.Errorf("create staff session: %w", err)
	}
	access, exp, err := k.Signer.Sign(token.Claims{
		Subject: m.ID(), SessionID: s.ID(), AuthTime: now, Version: 0, Audience: AudienceStaff, ExpiresAt: time.Time{},
	}, session.AccessTTL, now)
	if err != nil {
		return SignedIn{}, fmt.Errorf("sign staff access token: %w", err)
	}
	return SignedIn{
		SignIn: SignIn{AccessToken: access, AccessTokenExpiresAt: exp, TokenType: "Bearer", Staff: meOf(m)},
		cookie: issued{refresh: refresh, expiresAt: s.ExpiresAt()},
	}, nil
}

// EnrollTOTPHandler serves POST /v1/staff/login/totp-enrollment.
func EnrollTOTPHandler(members member.Store, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in TicketRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		enr, err := EnrollTOTP(r.Context(), members, k, in.Ticket, now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, enr)
	})
}

// VerifyTOTPHandler serves POST /v1/staff/login/totp and sets the refresh
// cookie.
func VerifyTOTPHandler(st Stores, k Keys, trusted []netip.Prefix, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in LoginTOTPRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		t := now()
		out, err := VerifyTOTP(r.Context(), st, k, in.Ticket, in.Code, httpx.ClientIP(r, trusted), in.UserAgent, t)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		setRefreshCookie(w, out.cookie, t)
		httpx.Respond(w, http.StatusOK, out.SignIn)
	})
}
