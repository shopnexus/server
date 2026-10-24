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
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
)

const maxUserAgent = 500

type LoginRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	NewPassword string `json:"new_password"`
	Code        string `json:"code"`
	UserAgent   string `header:"User-Agent" json:"-"`
}

// SignedIn is a finished sign-in: the member, and the token and end of the
// session for the cookie.
type SignedIn struct {
	Me        Me
	Token     string
	ExpiresAt time.Time
}

// Login signs a member in. Every call sends the email and password; the
// member decides what else the call needs. A temporary password needs
// new_password, which replaces it. A member without TOTP enrolls through
// EnrollTOTP and confirms with the first code; an enrolled member sends a
// code. A missing piece is a 428 that names it.
//
// The attempt is charged to the email's lock before anything is checked. A
// wrong password or code keeps the charge; any other answer takes it back,
// and a finished sign-in clears the count.
func Login(ctx context.Context, st Stores, k Keys, in LoginRequest, ip netip.Addr, now time.Time) (SignedIn, error) {
	m, key, err := checkPassword(ctx, st.Members, st.Locks, k, in.Email, in.Password, now)
	if err != nil {
		return SignedIn{}, err
	}
	if m.Next() == member.StepChangePassword {
		if in.NewPassword == "" {
			return SignedIn{}, refund(ctx, st.Locks, key, now, ErrPasswordChangeRequired)
		}
		if policyErr := m.CheckNewPassword(in.NewPassword); policyErr != nil {
			return SignedIn{}, refund(ctx, st.Locks, key, now, policyErr)
		}
		m.SetPassword(k.Hasher.Hash(in.NewPassword), now)
		if err = st.Members.Save(ctx, &m, uuid.NewV7()); err != nil {
			return SignedIn{}, saveFailed(err)
		}
	}
	switch {
	case m.Next() == member.StepEnrollTOTP && !m.Enrolling():
		return SignedIn{}, refund(ctx, st.Locks, key, now, ErrTOTPEnrollmentRequired)
	case in.Code == "":
		return SignedIn{}, refund(ctx, st.Locks, key, now, ErrTOTPRequired)
	}
	if m.AcceptCode(in.Code, now) != nil {
		if err = st.Locks.Fail(ctx, key, now); err != nil {
			return SignedIn{}, fmt.Errorf("record failed sign-in: %w", err)
		}
		return SignedIn{}, member.ErrInvalidTOTPCode
	}
	// Saving the accepted step is what stops the code working twice.
	if err = st.Members.Save(ctx, &m, uuid.NewV7()); err != nil {
		return SignedIn{}, saveFailed(err)
	}
	if err = st.Locks.Clear(ctx, key, now); err != nil {
		return SignedIn{}, fmt.Errorf("clear login lock: %w", err)
	}
	if r := []rune(in.UserAgent); len(r) > maxUserAgent {
		in.UserAgent = string(r[:maxUserAgent])
	}
	tok, hash := token.NewOpaque("ss_")
	s := Session{
		ID: uuid.NewV7(), StaffID: m.ID(), TokenHash: hash, CredentialsVersion: m.CredentialsVersion(),
		IP: ip, UserAgent: in.UserAgent, CreatedAt: now, ExpiresAt: now.Add(SessionTTL),
	}
	if err = st.Sessions.Create(ctx, s); err != nil {
		return SignedIn{}, fmt.Errorf("create staff session: %w", err)
	}
	return SignedIn{Me: meOf(m), Token: tok, ExpiresAt: s.ExpiresAt}, nil
}

// checkPassword charges a sign-in attempt to the email's lock, then checks
// the password. An unknown email, a wrong password and a disabled member
// fail alike and keep the charge; an unknown email still costs one argon2
// run, so the response time does not tell them apart either. On success the
// charge is still counted, and the caller refunds or keeps it.
func checkPassword(ctx context.Context, members member.Store, locks LockStore, k Keys, email, password string, now time.Time) (member.Member, []byte, error) {
	email = contact.NormalizeEmail(email)
	key := lockKey(k.Locks, email)
	if err := locks.Charge(ctx, key, now); err != nil {
		return member.Member{}, nil, fmt.Errorf("charge login lock: %w", err)
	}
	m, err := members.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, member.ErrNotFound) {
		return member.Member{}, nil, fmt.Errorf("find staff member: %w", err)
	}
	hash := k.dummy
	if err == nil {
		hash = m.PasswordHash()
	}
	if !member.VerifyPassword(hash, password) || err != nil || m.Status() != member.StatusActive {
		if failErr := locks.Fail(ctx, key, now); failErr != nil {
			return member.Member{}, nil, fmt.Errorf("record failed sign-in: %w", failErr)
		}
		return member.Member{}, nil, member.ErrInvalidCredentials
	}
	return m, key, nil
}

// refund takes back the charge of an attempt whose password was right and
// returns refusal.
func refund(ctx context.Context, locks LockStore, key []byte, now time.Time, refusal error) error {
	if err := locks.Refund(ctx, key, now); err != nil {
		return fmt.Errorf("refund login lock: %w", err)
	}
	return refusal
}

// saveFailed is the answer to a failed member save.
func saveFailed(err error) error {
	if errors.Is(err, member.ErrConflict) {
		return apperr.ErrTryAgain
	}
	return fmt.Errorf("save staff member: %w", err)
}

// LoginHandler serves POST /v1/staff/login and sets the session cookie.
func LoginHandler(st Stores, k Keys, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in LoginRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		t := now()
		out, err := Login(r.Context(), st, k, in, httpx.ClientIP(r), t)
		if err != nil {
			logRefusal(r, log, in.Email, err)
			httpx.WriteError(w, r, log, err)
			return
		}
		setSessionCookie(w, out.Token, out.ExpiresAt.Sub(t))
		httpx.Respond(w, http.StatusOK, out.Me)
	})
}

// logRefusal logs a wrong password or code and a locked email, the
// sign-in events worth reading after an attack.
func logRefusal(r *http.Request, log *slog.Logger, email string, err error) {
	if errors.Is(err, member.ErrInvalidCredentials) || errors.Is(err, member.ErrInvalidTOTPCode) || errors.Is(err, ErrLoginLocked) {
		log.WarnContext(r.Context(), "security: staff sign-in refused",
			"email", contact.NormalizeEmail(email), "ip", httpx.ClientIP(r), "err", err)
	}
}
