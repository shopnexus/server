package e2e

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/provider/firebase/firebasetest"
	"github.com/shopnexus/server2/internal/provider/google/googletest"
)

const reauthPath = "/v1/auth/sessions/current/reauthenticate"

type accessToken struct {
	AccessToken          string `json:"access_token"`
	AccessTokenExpiresAt string `json:"access_token_expires_at"`
	TokenType            string `json:"token_type"`
}

func phoneProof(phone string) map[string]string {
	return map[string]string{"phone_token": issuer.SMSToken(phone)}
}

func googleProof(addr string) map[string]string {
	return map[string]string{"google_token": google.Token(addr)}
}

func emailProof(challengeID, code string) map[string]any {
	return map[string]any{"email_code": map[string]string{"challenge_id": challengeID, "code": code}}
}

// reauth sends body to the reauthentication with token and a fresh key.
func reauth(t *testing.T, token string, body any) Response {
	t.Helper()
	return call(t, "POST", reauthPath, body, bearer(token), newKey())
}

// newGoogleAccount signs up with a Google token for a fresh address, which
// has had no email code yet.
func newGoogleAccount(t *testing.T) (account signIn, addr string) {
	t.Helper()
	addr = newEmail()
	return expect[signIn](t, call(t, "POST", "/v1/auth/sign-in", googleProof(addr), newKey()), 200), addr
}

// staleSession moves the sign-in of me's session 20 minutes back, in its
// row and in the access token it returns, so a refresh does not renew it.
func staleSession(t *testing.T, me signIn) string {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE sessions SET auth_time = now() - interval '20 minutes' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": sessionOf(t, me.AccessToken)}); err != nil {
		t.Fatal(err)
	}
	return staleToken(t, me)
}

func authTimeOf(t *testing.T, token string) time.Time {
	t.Helper()
	p, err := accountdomain.NewVerifier(keys).Verify(token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return p.AuthTime
}

// identityCount is how many identities hold value.
func identityCount(t *testing.T, value string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM identities WHERE identifier = @value`,
		pgx.StrictNamedArgs{"value": value}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A phone token of the caller's account opens a call that needs a recent
// sign-in. The new access token is of the same session, the refresh token
// is not rotated, and the next refresh keeps the new sign-in time.
func TestReauthenticateByPhone(t *testing.T) {
	t.Parallel()
	me, phone := newAccount(t)
	other := signInWith(t, phone)
	stale := staleSession(t, me)
	signOutOthers := func(token string) Response { return call(t, "DELETE", "/v1/auth/sessions/others", nil, bearer(token)) }
	check(t, "before", signOutOthers(stale), 428, "reauth_required")
	refreshed := expect[tokens](t, refresh(t, me.RefreshToken), 200)
	check(t, "after a refresh alone", signOutOthers(refreshed.AccessToken), 428, "reauth_required")

	got := expect[accessToken](t, reauth(t, stale, phoneProof(phone)), 200)
	if got.TokenType != "Bearer" || got.AccessTokenExpiresAt == "" || sessionOf(t, got.AccessToken) != sessionOf(t, me.AccessToken) ||
		time.Since(authTimeOf(t, got.AccessToken)) > time.Minute {
		t.Fatalf("%+v", got)
	}
	expectStatus(t, signOutOthers(got.AccessToken), 204)
	if res := refresh(t, other.RefreshToken); res.Status != 401 || errorOf(t, res).Code != "session_revoked" {
		t.Fatalf("the other device: %d %s", res.Status, res.Body)
	}
	next := expect[tokens](t, refresh(t, refreshed.RefreshToken), 200)
	if time.Since(authTimeOf(t, next.AccessToken)) > time.Minute {
		t.Fatalf("a refresh after the reauthentication signed in at %v", authTimeOf(t, next.AccessToken))
	}
}

func TestReauthenticateByGoogle(t *testing.T) {
	t.Parallel()
	me, addr := newGoogleAccount(t)
	stale := staleSession(t, me)
	check(t, "before", call(t, "DELETE", "/v1/auth/sessions/others", nil, bearer(stale)), 428, "reauth_required")
	got := expect[accessToken](t, reauth(t, stale, googleProof(addr)), 200)
	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/others", nil, bearer(got.AccessToken)), 204)
}

// An email code from POST /v1/auth/email-codes reauthenticates once. A key
// replayed with the right code answers as the first request did, with its
// sign-in time; replayed with a wrong one, it gets code_expired.
func TestReauthenticateByEmailCode(t *testing.T) {
	t.Parallel()
	me, addr := newGoogleAccount(t)
	stale := staleSession(t, me)
	addPhone := func(token string) Response {
		return call(t, "POST", "/v1/accounts/me/identities", phoneProof(newPhone()), newKey(), bearer(token))
	}
	check(t, "before", addPhone(stale), 428, "reauth_required")
	id, code := sendEmailCode(t, addr)
	key := newKey()
	first := expect[accessToken](t, call(t, "POST", reauthPath, emailProof(id, code), key, bearer(stale)), 200)
	check(t, "replayed with a wrong code", call(t, "POST", reauthPath, emailProof(id, wrongCode(code)), key, bearer(stale)), 422, "code_expired")
	again := expect[accessToken](t, call(t, "POST", reauthPath, emailProof(id, code), key, bearer(stale)), 200)
	if !authTimeOf(t, again.AccessToken).Equal(authTimeOf(t, first.AccessToken)) {
		t.Fatalf("the replay signed in at %v, the first at %v", authTimeOf(t, again.AccessToken), authTimeOf(t, first.AccessToken))
	}
	check(t, "the code under a new key", reauth(t, stale, emailProof(id, code)), 422, "code_expired")
	expectStatus(t, addPhone(first.AccessToken), 200)
}

// A proof of another account is refused, whichever kind it is.
func TestReauthenticateWithAnotherAccountsProof(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	_, theirPhone := newAccount(t)
	_, theirAddr := newGoogleAccount(t)
	check(t, "their phone", reauth(t, me.AccessToken, phoneProof(theirPhone)), 403, "wrong_account")
	check(t, "their google", reauth(t, me.AccessToken, googleProof(theirAddr)), 403, "wrong_account")
	id, code := sendEmailCode(t, theirAddr)
	check(t, "their email code", reauth(t, me.AccessToken, emailProof(id, code)), 403, "wrong_account")
}

// A proof of an address no account holds is refused like another
// account's, and creates no account and no identity.
func TestReauthenticateCreatesNoAccount(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	phone, addr := newPhone(), newEmail()
	check(t, "unknown phone", reauth(t, me.AccessToken, phoneProof(phone)), 403, "wrong_account")
	check(t, "unknown google", reauth(t, me.AccessToken, googleProof(addr)), 403, "wrong_account")
	id, code := sendEmailCode(t, addr)
	check(t, "unknown email", reauth(t, me.AccessToken, emailProof(id, code)), 403, "wrong_account")
	for _, v := range []string{phone, addr} {
		if n := identityCount(t, v); n != 0 {
			t.Fatalf("%s has %d identities", v, n)
		}
	}
	if mine := expect[myAccount](t, call(t, "GET", "/v1/accounts/me", nil, bearer(me.AccessToken)), 200); len(mine.Identities) != 1 {
		t.Fatalf("the caller's identities: %+v", mine.Identities)
	}
	if later := expect[signIn](t, call(t, "POST", "/v1/auth/sign-in", googleProof(addr), newKey()), 200); !later.Account.IsNew {
		t.Fatalf("the address signs in to %s", later.Account.ID)
	}
}

// The session and the account must be in order, and the proof must pass
// the checks a sign-in makes, before the proof counts.
func TestReauthenticateRefusals(t *testing.T) {
	t.Parallel()
	me, phone := newAccount(t)
	check(t, "signed out", call(t, "POST", reauthPath, phoneProof(phone), newKey()), 401, "unauthenticated")
	check(t, "no key", call(t, "POST", reauthPath, phoneProof(phone), bearer(me.AccessToken)), 400, "")
	check(t, "no proof", reauth(t, me.AccessToken, map[string]string{}), 400, "invalid_request")
	check(t, "two proofs", reauth(t, me.AccessToken, map[string]string{"phone_token": issuer.SMSToken(phone), "google_token": google.Token(newEmail())}),
		400, "invalid_request")
	check(t, "a device name", reauth(t, me.AccessToken, map[string]string{"phone_token": issuer.SMSToken(phone), "device_name": "e2e"}),
		400, "invalid_request")
	check(t, "forged phone token", reauth(t, me.AccessToken, map[string]string{"phone_token": issuer.SMSToken(phone, firebasetest.Claim("aud", "other"))}),
		422, "invalid_phone_token")
	check(t, "stale google token", reauth(t, me.AccessToken,
		map[string]string{"google_token": google.Token(newEmail(), googletest.Claim("iat", time.Now().Add(-6*time.Minute).Unix()))}),
		422, "invalid_google_token")
	check(t, "foreign number", reauth(t, me.AccessToken, phoneProof("+14155550100")), 422, "invalid_phone")
	check(t, "pnv keys down", reauth(t, me.AccessToken, map[string]string{"phone_token": issuer.PNVToken(phone)}), 503, "unavailable")

	byGoogle, addr := newGoogleAccount(t)
	id, code := sendEmailCode(t, addr)
	res := reauth(t, byGoogle.AccessToken, emailProof(id, wrongCode(code)))
	if e := errorOf(t, res); res.Status != 422 || e.Code != "invalid_code" || e.Details["attempts_left"] != float64(4) {
		t.Fatalf("a wrong code: %d %s", res.Status, res.Body)
	}
	for range accountdomain.MaxAttempts - 2 {
		check(t, "another wrong code", reauth(t, byGoogle.AccessToken, emailProof(id, wrongCode(code))), 422, "invalid_code")
	}
	check(t, "the last wrong code", reauth(t, byGoogle.AccessToken, emailProof(id, wrongCode(code))), 422, "code_locked")
	check(t, "the right code after", reauth(t, byGoogle.AccessToken, emailProof(id, code)), 422, "code_locked")

	suspended, suspendedPhone := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "suspended", reauth(t, suspended.AccessToken, phoneProof(suspendedPhone)), 403, "account_suspended")

	signedOut, signedOutPhone := newAccount(t)
	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/current", nil, bearer(signedOut.AccessToken)), 204)
	check(t, "a signed-out session", reauth(t, signedOut.AccessToken, phoneProof(signedOutPhone)), 401, "session_revoked")

	expired, expiredPhone := newAccount(t)
	if _, err := db.Exec(context.Background(), `UPDATE sessions SET expires_at = now() - interval '1 second' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": sessionOf(t, expired.AccessToken)}); err != nil {
		t.Fatal(err)
	}
	check(t, "an expired session", reauth(t, expired.AccessToken, phoneProof(expiredPhone)), 401, "session_expired")
}

// Parallel reauthentications with one code race to count it: one wins, and
// each other gets try_again or code_expired.
func TestReauthenticateCodeRace(t *testing.T) {
	t.Parallel()
	for range 10 {
		me, addr := newGoogleAccount(t)
		id, code := sendEmailCode(t, addr)
		statuses := make([]int, 8)
		var wg sync.WaitGroup
		for i := range statuses {
			wg.Go(func() {
				res := reauth(t, me.AccessToken, emailProof(id, code))
				if e := errorOf(t, res); (res.Status == 409 && e.Code != "try_again") || (res.Status == 422 && e.Code != "code_expired") {
					t.Errorf("%d %s", res.Status, res.Body)
				}
				statuses[i] = res.Status
			})
		}
		wg.Wait()
		won := 0
		for _, s := range statuses {
			switch s {
			case 200:
				won++
			case 409, 422:
			default:
				t.Fatalf("statuses %v", statuses)
			}
		}
		if won != 1 {
			t.Fatalf("statuses %v", statuses)
		}
		if slices.Contains(statuses, 409) {
			return
		}
	}
	t.Fatal("ten rounds of 8 parallel reauthentications never raced")
}
