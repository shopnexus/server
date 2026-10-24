package staff_test

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
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
)

var (
	now      = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	clientIP = netip.MustParseAddr("203.0.113.7")
)

const password = "a long staff password"

type env struct {
	db     *pgxpool.Pool
	keys   staff.Keys
	stores staff.Stores
}

func newEnv(t *testing.T) env {
	t.Helper()
	k, err := staff.NewKeys(config.StaffConfig{Key: config.NewStaffKey(), ConsoleOrigin: "http://console.test"}, member.Fast)
	if err != nil {
		t.Fatal(err)
	}
	db := pgtest.New(t)
	return env{db: db, keys: k, stores: staff.NewStores(db, k)}
}

func (e env) login(email, pw, newPassword, code string, at time.Time) (staff.SignedIn, error) {
	in := staff.LoginRequest{Email: email, Password: pw, NewPassword: newPassword, Code: code, UserAgent: "test"}
	return staff.Login(context.Background(), e.stores, e.keys, in, clientIP, at)
}

func (e env) enroll(email, pw string, at time.Time) (staff.Enrollment, error) {
	return staff.EnrollTOTP(context.Background(), e.stores.Members, e.stores.Locks, e.keys, email, pw, at)
}

// callerOf is the authn.Staff that staff.Require puts in the context for
// the session of a sign-in.
func (e env) callerOf(t *testing.T, in staff.SignedIn) authn.Staff {
	t.Helper()
	g, err := e.stores.Sessions.Live(context.Background(), token.Hash(in.Token), now)
	if err != nil {
		t.Fatal(err)
	}
	return authn.Staff{StaffID: g.StaffID, SessionID: g.SessionID, IP: clientIP}
}

// create makes a member and returns its view and temporary password.
func (e env) create(t *testing.T, email string, role member.Role) (v staff.MemberView, temp string) {
	t.Helper()
	v, temp, err := staff.CreateMember(context.Background(), e.stores.Members, e.keys.Hasher, email, "Mod One", role, audit.ByCLI("test"), now)
	if err != nil {
		t.Fatal(err)
	}
	return v, temp
}

// enrolled makes a moderator that has replaced its temporary password with
// password, enrolled TOTP and signed in at now, and returns the secret and
// the sign-in.
func (e env) enrolled(t *testing.T, email string) (secret []byte, in staff.SignedIn) {
	t.Helper()
	_, temp := e.create(t, email, member.RoleModerator)
	if _, err := e.login(email, temp, password, "", now); err == nil {
		t.Fatal("a first sign-in finished without TOTP")
	}
	enr, err := e.enroll(email, password, now)
	if err != nil {
		t.Fatal(err)
	}
	secret = decodeSecret(t, enr.Secret)
	if in, err = e.login(email, password, "", member.Code(secret, now), now); err != nil {
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
