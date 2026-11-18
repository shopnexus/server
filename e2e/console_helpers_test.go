package e2e

import (
	"context"
	"encoding/base32"
	"fmt"
	"net/http"
	"testing"
	"time"

	consoledomain "github.com/shopnexus/server/internal/console/domain"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/audit"
)

const memberPassword = "a long e2e member password"

func newMemberEmail() string { return fmt.Sprintf("member-%d@example.com", seq.Add(1)) }

// newMember creates a member the way cmd/console does and returns its email and
// temporary password.
func newMember(t *testing.T, role consoledomain.Role) (id, email, temp string) {
	t.Helper()
	email = newMemberEmail()
	v, temp, err := consoleSvc.CreateMember(context.Background(), consolesvc.CreateMemberInput{
		Email: email, DisplayName: "E2E Member", Role: role, By: audit.ByCLI("e2e"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return v.ID.String(), email, temp
}

// consoleSession is a signed-in member; the zero value is a caller with no
// session.
type consoleSession struct {
	ID     string
	Email  string
	Secret []byte
	Token  string // the console_session cookie's value
}

// consoleCall calls the console API the way the console does: with its header
// and, for a signed-in session, the cookie.
func consoleCall(t *testing.T, s consoleSession, method, path string, body any, headers ...[2]string) Response {
	t.Helper()
	h := append([][2]string{{"X-Requested-With", "shopnexus-console"}}, headers...)
	if s.Token != "" {
		h = append(h, [2]string{"Cookie", "console_session=" + s.Token})
	}
	return call(t, method, path, body, h...)
}

func consoleLogin(t *testing.T, body map[string]string, headers ...[2]string) Response {
	t.Helper()
	return consoleCall(t, consoleSession{}, "POST", "/v1/console/login", body, headers...)
}

// signInMember creates a member and takes it through the first sign-in.
func signInMember(t *testing.T, role consoledomain.Role) consoleSession {
	t.Helper()
	id, email, temp := newMember(t, role)
	expectStatus(t, consoleLogin(t, map[string]string{"email": email, "password": temp, "new_password": memberPassword}), 428)
	res := consoleCall(t, consoleSession{}, "POST", "/v1/console/login/totp-enrollment", map[string]string{"email": email, "password": memberPassword})
	secret := decodeBase32(t, expect[struct {
		Secret string `json:"secret"`
	}](t, res, 200).Secret)
	res = consoleLogin(t, map[string]string{"email": email, "password": memberPassword, "code": consoledomain.Code(secret, time.Now())})
	expectStatus(t, res, 200)
	return consoleSession{ID: id, Email: email, Secret: secret, Token: sessionCookieOf(t, res).Value}
}

func sessionCookieOf(t *testing.T, res Response) *http.Cookie {
	t.Helper()
	for _, c := range (&http.Response{Header: res.Header}).Cookies() {
		if c.Name == "console_session" {
			return c
		}
	}
	t.Fatalf("no console_session cookie in %v", res.Header)
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
