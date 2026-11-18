package e2e

import (
	"testing"
	"time"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/provider/firebase/firebasetest"
	"github.com/shopnexus/server/internal/provider/google/googletest"
)

type myAccount struct {
	ID         string `json:"id"`
	Identities []struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"identities"`
}

func TestAddAPhoneToAnEmailAccount(t *testing.T) {
	t.Parallel()
	me, addr := newEmailAccount(t)
	before := expect[myAccount](t, call(t, "GET", "/v1/accounts/me", nil, bearer(me.AccessToken)), 200)
	if len(before.Identities) != 1 || before.Identities[0].Kind != "email" || before.Identities[0].Value != addr {
		t.Fatalf("%+v", before)
	}
	phone := newPhone()
	expectStatus(t, call(t, "POST", "/v1/accounts/me/identities", map[string]string{"phone_token": issuer.SMSToken(phone)}, newKey(), bearer(me.AccessToken)), 200)
	after := expect[myAccount](t, call(t, "GET", "/v1/accounts/me", nil, bearer(me.AccessToken)), 200)
	if len(after.Identities) != 2 {
		t.Fatalf("%+v", after)
	}
	if byPhone := signInWith(t, phone); byPhone.Account.ID != me.Account.ID {
		t.Fatalf("the phone signs in to %s", byPhone.Account.ID)
	}
}

func TestAddAnEmailToAPhoneAccount(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	addr := newEmail()
	c := expect[emailCode](t, call(t, "POST", "/v1/accounts/me/email-codes", map[string]string{"email": addr},
		newKey(), newClient(), bearer(me.AccessToken)), 200)
	expectStatus(t, call(t, "POST", "/v1/accounts/me/identities",
		map[string]any{"email_code": map[string]string{"challenge_id": c.ChallengeID, "code": mails.code(t, addr, 1)}},
		newKey(), bearer(me.AccessToken)), 200)
	byGoogle := expect[signIn](t, call(t, "POST", "/v1/auth/sign-in", map[string]string{"google_token": google.Token(addr)}, newKey()), 200)
	if byGoogle.Account.ID != me.Account.ID {
		t.Fatalf("google signs in to %s", byGoogle.Account.ID)
	}
}

func TestAddIdentityRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newEmailAccount(t)
	_, takenPhone := newAccount(t)
	add := func(body any, headers ...[2]string) Response {
		return call(t, "POST", "/v1/accounts/me/identities", body, headers...)
	}
	auth := bearer(me.AccessToken)
	check(t, "taken phone", add(map[string]string{"phone_token": issuer.SMSToken(takenPhone)}, newKey(), auth), 409, "identity_taken")
	check(t, "a second email", add(map[string]string{"google_token": google.Token(newEmail())}, newKey(), auth), 409, "identity_kind_taken")
	check(t, "foreign number", add(map[string]string{"phone_token": issuer.SMSToken("+14155550100")}, newKey(), auth), 422, "invalid_phone")
	check(t, "forged token", add(map[string]string{"phone_token": issuer.SMSToken(newPhone(), firebasetest.Claim("aud", "other"))}, newKey(), auth), 422, "invalid_phone_token")
	check(t, "stale google token", add(map[string]string{"google_token": google.Token(newEmail(), googletest.Claim("iat", time.Now().Add(-6*time.Minute).Unix()))},
		newKey(), auth), 422, "invalid_google_token")
	check(t, "no key", add(map[string]string{"phone_token": issuer.SMSToken(newPhone())}, auth), 400, "")
	check(t, "signed out", add(map[string]string{"phone_token": issuer.SMSToken(newPhone())}, newKey()), 401, "")
}

// staleToken is an access token of me's session whose sign-in was 20
// minutes ago.
func staleToken(t *testing.T, me signIn) string {
	t.Helper()
	p, err := accountdomain.NewVerifier(keys).Verify(me.AccessToken, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s, _ := accountdomain.NewSession(p.SessionID, p.AccountID, "", time.Now().Add(-20*time.Minute), time.Now())
	token, _, err := accountdomain.NewSigner(keys).Sign(s, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// Adding a sign-in method needs a recent sign-in and an account that may
// write, and so does mailing the code that adds an email.
func TestAddingAnIdentityNeedsARecentSignIn(t *testing.T) {
	t.Parallel()
	me, _ := newEmailAccount(t)
	stale := bearer(staleToken(t, me))
	check(t, "add, stale", call(t, "POST", "/v1/accounts/me/identities", map[string]string{"phone_token": issuer.SMSToken(newPhone())}, newKey(), stale),
		428, "reauth_required")
	check(t, "mail, stale", call(t, "POST", "/v1/accounts/me/email-codes", map[string]string{"email": newEmail()}, newKey(), newClient(), stale),
		428, "reauth_required")

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	auth := bearer(suspended.AccessToken)
	check(t, "add, suspended", call(t, "POST", "/v1/accounts/me/identities", map[string]string{"google_token": google.Token(newEmail())}, newKey(), auth),
		403, "account_suspended")
	check(t, "mail, suspended", call(t, "POST", "/v1/accounts/me/email-codes", map[string]string{"email": newEmail()}, newKey(), newClient(), auth),
		403, "account_suspended")
}
