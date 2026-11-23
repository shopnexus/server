package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// newEmail returns a fresh address.
func newEmail() string { return fmt.Sprintf("linh%d@e2e.test", seq.Add(1)) }

type emailCode struct {
	ChallengeID string `json:"challenge_id"`
	ExpiresAt   string `json:"expires_at"`
	ResendAt    string `json:"resend_at"`
}

// sendEmailCode mails a sign-in code to addr and returns its challenge and
// the code the inbox got.
func sendEmailCode(t *testing.T, addr string) (challengeID, code string) {
	t.Helper()
	n := mails.count(addr)
	c := expect[emailCode](t, call(t, "POST", "/v1/auth/email-codes", map[string]string{"email": addr}, newKey(), newClient()), 200)
	return c.ChallengeID, mails.code(t, addr, n+1)
}

func TestSendEmailCode(t *testing.T) {
	t.Parallel()
	addr := newEmail()
	id, code := sendEmailCode(t, addr)
	if id == "" || len(code) != 6 {
		t.Fatalf("challenge %q, code %q", id, code)
	}
	check(t, "resend", call(t, "POST", "/v1/auth/email-codes", map[string]string{"email": addr}, newKey(), newClient()), 429, "otp_too_soon")
	check(t, "invalid email", call(t, "POST", "/v1/auth/email-codes", map[string]string{"email": "not an address"}, newKey(), newClient()), 422, "invalid_email")
	check(t, "no key", call(t, "POST", "/v1/auth/email-codes", map[string]string{"email": newEmail()}, newClient()), 400, "")
}

func TestSendMyEmailCode(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	addr := newEmail()
	send := func(addr string, headers ...[2]string) Response {
		return call(t, "POST", "/v1/accounts/me/email-codes", map[string]string{"email": addr}, append(headers, newClient())...)
	}
	expect[emailCode](t, send(addr, newKey(), bearer(me.AccessToken)), 200)
	if mails.count(addr) != 1 {
		t.Fatalf("%d mails", mails.count(addr))
	}
	check(t, "resend", send(addr, newKey(), bearer(me.AccessToken)), 429, "otp_too_soon")
	check(t, "invalid email", send("x@y", newKey(), bearer(me.AccessToken)), 422, "invalid_email")
	check(t, "no key", send(newEmail(), bearer(me.AccessToken)), 400, "")
	check(t, "signed out", send(newEmail(), newKey()), 401, "")
}

// A caller whose Accept-Language prefers English gets the code in English,
// and signing in records English as the account's mail language.
func TestMailLanguageFollowsAcceptLanguage(t *testing.T) {
	t.Parallel()
	addr, english := newEmail(), [2]string{"Accept-Language", "en-US,en;q=0.9"}
	n := mails.count(addr)
	c := expect[emailCode](t, call(t, "POST", "/v1/auth/email-codes", map[string]string{"email": addr}, newKey(), newClient(), english), 200)
	if mail := mails.last(t, addr); !strings.HasPrefix(mail, "Your ShopNexus code: ") {
		t.Fatalf("mail %q", mail)
	}
	s := expect[signIn](t, call(t, "POST", "/v1/auth/sign-in",
		map[string]any{"email_code": map[string]string{"challenge_id": c.ChallengeID, "code": mails.code(t, addr, n+1)}, "device_name": "e2e"},
		newKey(), english), 200)
	var tag string
	if err := db.QueryRow(context.Background(), `SELECT locale FROM accounts WHERE id = $1`, s.Account.ID).Scan(&tag); err != nil || tag != "en" {
		t.Fatalf("locale %q %v", tag, err)
	}
}
