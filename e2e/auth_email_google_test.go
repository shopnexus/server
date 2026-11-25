package e2e

import (
	"testing"
	"time"

	"github.com/shopnexus/server/internal/provider/google/googletest"
)

func TestSignUpByEmailThenGoogle(t *testing.T) {
	t.Parallel()
	first, addr := newEmailAccount(t)
	if !first.Account.IsNew {
		t.Fatalf("%+v", first.Account)
	}
	byGoogle := expect[signIn](t, call(t, "POST", "/v1/auth/sign-in", map[string]string{"google_token": google.Token(addr)}, newKey()), 200)
	if byGoogle.Account.ID != first.Account.ID || byGoogle.Account.IsNew {
		t.Fatalf("google %+v", byGoogle.Account)
	}
}

func TestSignUpByGoogle(t *testing.T) {
	t.Parallel()
	res := expect[signIn](t, call(t, "POST", "/v1/auth/sign-in", map[string]string{"google_token": google.Token(newEmail())}, newKey()), 200)
	if !res.Account.IsNew {
		t.Fatalf("%+v", res.Account)
	}
}

// A key sent again with a wrong code gets no session, though the first
// request under it consumed the code.
func TestSignInByEmailReplayWithAWrongCode(t *testing.T) {
	t.Parallel()
	id, code := sendEmailCode(t, newEmail())
	key := newKey()
	proof := func(code string) map[string]any {
		return map[string]any{"email_code": map[string]string{"challenge_id": id, "code": code}}
	}
	expectStatus(t, call(t, "POST", "/v1/auth/sign-in", proof(code), key), 200)
	check(t, "replay", call(t, "POST", "/v1/auth/sign-in", proof(wrongCode(code)), key), 422, "code_expired")
}

func TestSignInByEmailRefusals(t *testing.T) {
	t.Parallel()
	addr := newEmail()
	id, code := sendEmailCode(t, addr)
	check(t, "wrong code", call(t, "POST", "/v1/auth/sign-in",
		map[string]any{"email_code": map[string]string{"challenge_id": id, "code": wrongCode(code)}}, newKey()), 422, "invalid_code")
	check(t, "two proofs", call(t, "POST", "/v1/auth/sign-in",
		map[string]any{"google_token": google.Token(addr), "email_code": map[string]string{"challenge_id": id, "code": code}}, newKey()), 400, "invalid_request")
	check(t, "no proof", call(t, "POST", "/v1/auth/sign-in", map[string]any{"device_name": "e2e"}, newKey()), 400, "invalid_request")
	check(t, "stale google token", call(t, "POST", "/v1/auth/sign-in",
		map[string]string{"google_token": google.Token(addr, googletest.Claim("iat", time.Now().Add(-6*time.Minute).Unix()))}, newKey()), 401, "invalid_google_token")
	check(t, "unverified google email", call(t, "POST", "/v1/auth/sign-in",
		map[string]string{"google_token": google.Token(addr, googletest.Claim("email_verified", false))}, newKey()), 401, "invalid_google_token")
}
