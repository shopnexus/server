package service_test

import (
	"context"
	"encoding/base32"
	"net/netip"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/service"
)

var (
	now      = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	clientIP = netip.MustParseAddr("203.0.113.7")
)

const password = "a long staff password"

type env struct {
	db   *pgxpool.Pool
	keys service.Keys
}

func newEnv(t *testing.T) env {
	t.Helper()
	k, err := service.NewKeys(config.StaffConfig{Key: config.NewStaffKey(), ConsoleOrigin: "http://console.test"}, domain.Fast)
	if err != nil {
		t.Fatal(err)
	}
	return env{db: pgtest.New(t), keys: k}
}

// at is Staff on the env's database with its clock stopped at t.
func (e env) at(t time.Time) *service.Staff {
	return staff.NewService(e.db, e.keys, nil, nil, func() time.Time { return t })
}

func (e env) login(email, pw, newPassword, code string, at time.Time) (service.SignedIn, error) {
	in := service.LoginInput{Email: email, Password: pw, NewPassword: newPassword, Code: code, UserAgent: "test", IP: clientIP}
	return e.at(at).Login(context.Background(), in)
}

func (e env) enroll(email, pw string, at time.Time) (service.Enrollment, error) {
	return e.at(at).EnrollTOTP(context.Background(), email, pw)
}

// callerOf is the authn.Staff that the handler's Require puts in the
// context for the session of a sign-in.
func (e env) callerOf(t *testing.T, in service.SignedIn) authn.Staff {
	t.Helper()
	g, err := e.at(now).Authenticate(context.Background(), in.Token)
	if err != nil {
		t.Fatal(err)
	}
	return authn.Staff{StaffID: g.StaffID, SessionID: g.SessionID, IP: clientIP}
}

// create makes a member and returns its view and temporary password.
func (e env) create(t *testing.T, email string, role domain.Role) (v service.MemberView, temp string) {
	t.Helper()
	v, temp, err := e.at(now).CreateMember(context.Background(), service.CreateMemberInput{
		Email: email, DisplayName: "Mod One", Role: role, By: audit.ByCLI("test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return v, temp
}

// enrolled makes a moderator that has replaced its temporary password with
// password, enrolled TOTP and signed in at now, and returns the secret and
// the sign-in.
func (e env) enrolled(t *testing.T, email string) (secret []byte, in service.SignedIn) {
	t.Helper()
	_, temp := e.create(t, email, domain.RoleModerator)
	if _, err := e.login(email, temp, password, "", now); err == nil {
		t.Fatal("a first sign-in finished without TOTP")
	}
	enr, err := e.enroll(email, password, now)
	if err != nil {
		t.Fatal(err)
	}
	secret = decodeSecret(t, enr.Secret)
	if in, err = e.login(email, password, "", domain.Code(secret, now), now); err != nil {
		t.Fatal(err)
	}
	return secret, in
}

func decodeSecret(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// wrong is a six-digit code that is not code.
func wrong(code string) string { return code[:5] + string(rune('0'+(code[5]-'0'+1)%10)) }
