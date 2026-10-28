package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/provider/firebase/firebasetest"
)

func TestSignUpThenSignIn(t *testing.T) {
	t.Parallel()
	first, phone := newAccount(t)
	if !first.Account.IsNew || first.TokenType != "Bearer" || first.AccessToken == "" || first.RefreshToken == "" {
		t.Fatalf("%+v", first)
	}
	if second := signInWith(t, phone); second.Account.ID != first.Account.ID || second.Account.IsNew {
		t.Fatalf("second %+v", second.Account)
	}
}

func TestSignInReplayedKeyGetsTheSameSession(t *testing.T) {
	t.Parallel()
	token, key := issuer.SMSToken(newPhone()), newKey()
	a, b := signInCall(t, token, key), signInCall(t, token, key)
	sa, sb := decode[signIn](t, a), decode[signIn](t, b)
	if a.Status != 200 || b.Status != 200 || sa.Account != sb.Account || sb.RefreshToken == sa.RefreshToken {
		t.Fatalf("a %d %s, b %d %s", a.Status, a.Body, b.Status, b.Body)
	}
	if res := refresh(t, sb.RefreshToken); res.Status != 200 {
		t.Fatalf("the replay's refresh token: %d %s", res.Status, res.Body)
	}
}

// A key someone else used, sent with a token for another number, runs on
// its own and never returns the first number's session.
func TestSignInKeyIsBoundToTheNumber(t *testing.T) {
	t.Parallel()
	key := newKey()
	victim := decode[signIn](t, signInCall(t, issuer.SMSToken(newPhone()), key))
	res := signInCall(t, issuer.SMSToken(newPhone()), key)
	if res.Status != 200 || decode[signIn](t, res).Account.ID == victim.Account.ID {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
}

// Replaying the sign-in of a signed-out session must not bring it back.
func TestSignInReplayAfterLogoutIsRefused(t *testing.T) {
	t.Parallel()
	token, key := issuer.SMSToken(newPhone()), newKey()
	first := decode[signIn](t, signInCall(t, token, key))
	if res := call(t, server, "POST", "/v1/auth/logout", nil, bearer(first.AccessToken)); res.Status != 204 {
		t.Fatalf("logout %d", res.Status)
	}
	if res := signInCall(t, token, key); res.Status != 401 || errorOf(t, res).Code != "session_revoked" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
}

func TestSignInSuspendedAccount(t *testing.T) {
	t.Parallel()
	a, phone := newAccount(t)
	suspend(t, a.Account.ID)
	if res := signInCall(t, issuer.SMSToken(phone)); res.Status != 403 || errorOf(t, res).Code != "account_suspended" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
}

func TestSignInRefusals(t *testing.T) {
	t.Parallel()
	stranger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	phone := newPhone()
	for name, c := range map[string]struct {
		body   map[string]string
		status int
		code   string
	}{
		"forged":         {map[string]string{"phone_token": issuer.SMSToken(phone, firebasetest.SignedWith(jwt.SigningMethodRS256, stranger))}, 401, "invalid_phone_token"},
		"stale":          {map[string]string{"phone_token": issuer.SMSToken(phone, firebasetest.Claim("auth_time", time.Now().Add(-6*time.Minute).Unix()))}, 401, "invalid_phone_token"},
		"foreign number": {map[string]string{"phone_token": issuer.SMSToken("+14155550100")}, 422, "invalid_phone"},
		"pnv keys down":  {map[string]string{"phone_token": issuer.PNVToken(phone)}, 503, "unavailable"},
		"empty token":    {map[string]string{"phone_token": ""}, 400, "invalid_request"},
		"unknown field":  {map[string]string{"phone_token": "x", "code": "123456"}, 400, "invalid_request"},
	} {
		if res := call(t, server, "POST", "/v1/auth/sign-in", c.body, newKey()); res.Status != c.status || errorOf(t, res).Code != c.code {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	if res := call(t, server, "POST", "/v1/auth/sign-in", map[string]string{"phone_token": issuer.SMSToken(phone)}); res.Status != 400 {
		t.Errorf("no Idempotency-Key: %d %s", res.Status, res.Body)
	}
}

// Parallel first sign-ins of one number race to create its account: one
// wins, the others get try_again, and the number ends with one account.
func TestSignInRace(t *testing.T) {
	t.Parallel()
	for range 10 {
		phone := newPhone()
		token := issuer.SMSToken(phone)
		statuses := make([]int, 16)
		var wg sync.WaitGroup
		for i := range statuses {
			wg.Go(func() { statuses[i] = signInStatus(token) })
		}
		wg.Wait()
		for _, s := range statuses {
			if s != 200 && s != 409 {
				t.Fatalf("statuses %v", statuses)
			}
		}
		var n int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM identities WHERE identifier = @phone`,
			pgx.StrictNamedArgs{"phone": phone}).Scan(&n); err != nil || n != 1 {
			t.Fatalf("identities of %s: %d %v", phone, n, err)
		}
		if slices.Contains(statuses, 409) {
			return
		}
	}
	t.Fatal("ten rounds of 16 parallel sign-ins never raced")
}
