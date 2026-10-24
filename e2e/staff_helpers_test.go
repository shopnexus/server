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

func newStaffEmail() string { return fmt.Sprintf("staff-%d@example.com", seq.Add(1)) }

// newStaff creates a member the way cmd/staff does and returns its email and
// temporary password.
func newStaff(t *testing.T, role member.Role) (id, email, temp string) {
	t.Helper()
	email = newStaffEmail()
	v, temp, err := staff.CreateMember(context.Background(), staffStores.Members, staff.FastHasher, email, "E2E Staff", role, audit.ByCLI("e2e"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return v.ID.String(), email, temp
}

type staffSession struct {
	ID       string
	Email    string
	Password string
	Secret   []byte
	Access   string
	Refresh  string // the staff_refresh cookie's value
}

type loginStep struct {
	Ticket string `json:"ticket"`
	Next   string `json:"next"`
}

type staffSignIn struct {
	AccessToken string `json:"access_token"`
	Staff       struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"staff"`
}

func staffLogin(t *testing.T, email, password string) Response {
	t.Helper()
	return call(t, server, "POST", "/v1/staff/login", map[string]string{"email": email, "password": password})
}

// signInStaff creates a member and takes it through the first sign-in.
func signInStaff(t *testing.T, role member.Role) staffSession {
	t.Helper()
	id, email, temp := newStaff(t, role)
	res := staffLogin(t, email, temp)
	if res.Status != 200 {
		t.Fatalf("login: %d %s", res.Status, res.Body)
	}
	step := decode[loginStep](t, res)
	password := "a long e2e staff password"
	res = call(t, server, "POST", "/v1/staff/login/password", map[string]string{"ticket": step.Ticket, "new_password": password})
	if res.Status != 200 {
		t.Fatalf("password: %d %s", res.Status, res.Body)
	}
	step = decode[loginStep](t, res)
	res = call(t, server, "POST", "/v1/staff/login/totp-enrollment", map[string]string{"ticket": step.Ticket})
	if res.Status != 200 {
		t.Fatalf("enrollment: %d %s", res.Status, res.Body)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(decode[struct {
		Secret string `json:"secret"`
	}](t, res).Secret)
	if err != nil {
		t.Fatal(err)
	}
	res = call(t, server, "POST", "/v1/staff/login/totp", map[string]string{"ticket": step.Ticket, "code": member.Code(secret, time.Now())})
	if res.Status != 200 {
		t.Fatalf("totp: %d %s", res.Status, res.Body)
	}
	return staffSession{
		ID: id, Email: email, Password: password, Secret: secret,
		Access: decode[staffSignIn](t, res).AccessToken, Refresh: refreshCookieOf(t, res).Value,
	}
}

func refreshCookieOf(t *testing.T, res Response) *http.Cookie {
	t.Helper()
	for _, c := range (&http.Response{Header: res.Header}).Cookies() {
		if c.Name == "staff_refresh" {
			return c
		}
	}
	t.Fatalf("no staff_refresh cookie in %v", res.Header)
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

func staffBearer(s staffSession) [2]string { return bearer(s.Access) }

func refreshWith(t *testing.T, cookie string, headers ...[2]string) Response {
	t.Helper()
	h := append([][2]string{{"Cookie", "staff_refresh=" + cookie}}, headers...)
	return call(t, server, "POST", "/v1/staff/session/refresh", nil, h...)
}

var csrf = [2]string{"X-Requested-With", "shopnexus-console"}
