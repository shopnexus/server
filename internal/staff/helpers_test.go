package staff_test

import (
	"context"
	"encoding/base32"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/session"
)

var (
	now      = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	clientIP = netip.MustParseAddr("203.0.113.7")
)

type env struct {
	db     *pgxpool.Pool
	keys   staff.Keys
	stores staff.Stores
}

func newEnv(t *testing.T) env {
	t.Helper()
	auth := config.NewAuthKeys()
	auth.Issuer, auth.Audience = "server2-test", "shopnexus-app-test"
	k, err := staff.NewKeys(config.StaffConfig{Key: config.NewStaffKey(), ConsoleOrigin: "http://console.test"}, auth, staff.FastHasher)
	if err != nil {
		t.Fatal(err)
	}
	db := pgtest.New(t)
	return env{db: db, keys: k, stores: staff.NewStores(db, k)}
}

// sessionOf reads the session ID from a sign-in's access token.
func (e env) sessionOf(t *testing.T, in staff.SignedIn) uuid.UUID {
	t.Helper()
	issuedAt := in.SignIn.AccessTokenExpiresAt.Add(-session.AccessTTL)
	c, err := e.keys.Verifier.Verify(in.SignIn.AccessToken, staff.AudienceStaff, issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	return c.SessionID
}

// callerOf is the authn.Staff that staff.Require would put in the context.
func (e env) callerOf(t *testing.T, in staff.SignedIn) authn.Staff {
	t.Helper()
	return authn.Staff{StaffID: in.SignIn.Staff.ID, SessionID: e.sessionOf(t, in), Role: string(in.SignIn.Staff.Role), IP: clientIP}
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

// enrolled makes a moderator that has changed its password, enrolled TOTP
// and signed in at now, and returns the password, the secret and the sign-in.
func (e env) enrolled(t *testing.T, email string) (password string, secret []byte, in staff.SignedIn) {
	t.Helper()
	ctx := context.Background()
	_, temp := e.create(t, email, member.RoleModerator)
	step, err := staff.Login(ctx, e.stores.Members, e.stores.Quotas, e.keys, email, temp, clientIP, now)
	if err != nil {
		t.Fatal(err)
	}
	password = "a long staff password"
	if step, err = staff.ChangeTemporaryPassword(ctx, e.stores.Members, e.keys, step.Ticket, password, clientIP, now); err != nil {
		t.Fatal(err)
	}
	enr, err := staff.EnrollTOTP(ctx, e.stores.Members, e.keys, step.Ticket, now)
	if err != nil {
		t.Fatal(err)
	}
	secret = decodeSecret(t, enr.Secret)
	if in, err = staff.VerifyTOTP(ctx, e.stores, e.keys, step.Ticket, member.Code(secret, now), clientIP, "test", now); err != nil {
		t.Fatal(err)
	}
	return password, secret, in
}

func decodeSecret(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
