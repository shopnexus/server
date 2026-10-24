package e2e

import (
	"context"
	"encoding/base32"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
)

const staffPassword = "a long e2e staff password"

func newStaffEmail() string { return fmt.Sprintf("staff-%d@example.com", seq.Add(1)) }

// newStaff creates a member the way cmd/staff does and returns its email and
// temporary password.
func newStaff(t *testing.T, role member.Role) (id, email, temp string) {
	t.Helper()
	email = newStaffEmail()
	v, temp, err := staff.CreateMember(context.Background(), staffStores.Members, member.Fast, email, "E2E Staff", role, audit.ByCLI("e2e"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return v.ID.String(), email, temp
}

// staffSession is a signed-in member; the zero value is a caller with no
// session.
type staffSession struct {
	ID     string
	Email  string
	Secret []byte
	Token  string // the staff_session cookie's value
}

// staffCall calls the staff API the way the console does: with its header
// and, for a signed-in session, the cookie.
func staffCall(t *testing.T, s staffSession, method, path string, body any, headers ...[2]string) Response {
	t.Helper()
	h := append([][2]string{{"X-Requested-With", "shopnexus-console"}}, headers...)
	if s.Token != "" {
		h = append(h, [2]string{"Cookie", "staff_session=" + s.Token})
	}
	return call(t, server, method, path, body, h...)
}

func staffLogin(t *testing.T, body map[string]string, headers ...[2]string) Response {
	t.Helper()
	return staffCall(t, staffSession{}, "POST", "/v1/staff/login", body, headers...)
}

// signInStaff creates a member and takes it through the first sign-in.
func signInStaff(t *testing.T, role member.Role) staffSession {
	t.Helper()
	id, email, temp := newStaff(t, role)
	if res := staffLogin(t, map[string]string{"email": email, "password": temp, "new_password": staffPassword}); res.Status != 428 {
		t.Fatalf("new password: %d %s", res.Status, res.Body)
	}
	res := staffCall(t, staffSession{}, "POST", "/v1/staff/login/totp-enrollment", map[string]string{"email": email, "password": staffPassword})
	if res.Status != 200 {
		t.Fatalf("enrollment: %d %s", res.Status, res.Body)
	}
	secret := decodeBase32(t, decode[struct {
		Secret string `json:"secret"`
	}](t, res).Secret)
	res = staffLogin(t, map[string]string{"email": email, "password": staffPassword, "code": member.Code(secret, time.Now())})
	if res.Status != 200 {
		t.Fatalf("code: %d %s", res.Status, res.Body)
	}
	return staffSession{ID: id, Email: email, Secret: secret, Token: sessionCookieOf(t, res).Value}
}

func sessionCookieOf(t *testing.T, res Response) *http.Cookie {
	t.Helper()
	for _, c := range (&http.Response{Header: res.Header}).Cookies() {
		if c.Name == "staff_session" {
			return c
		}
	}
	t.Fatalf("no staff_session cookie in %v", res.Header)
	return nil
}

func decodeBase32(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
