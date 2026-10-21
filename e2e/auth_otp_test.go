package e2e

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
)

func TestOTPSendDeliversACode(t *testing.T) {
	phone := newPhone()
	res := sendOTP(t, "phone", phone)
	if c := decode[challenge](t, res); res.Status != 200 || c.ChallengeID == "" || c.ResendAfter != 45 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	if box.code(t, phone, 1) == "" {
		t.Fatal("the message carries no code")
	}
}

func TestOTPSendEmail(t *testing.T) {
	addr := newEmail()
	if res := sendOTP(t, "email", addr); res.Status != 200 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	box.code(t, addr, 1)
}

func TestOTPSendTooSoon(t *testing.T) {
	phone := newPhone()
	sendOTP(t, "phone", phone)
	res := sendOTP(t, "phone", phone)
	if e := errorOf(t, res); res.Status != 429 || e.Code != "otp_too_soon" || e.Details["retry_after"] == nil {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestOTPSendLimitPerAddress(t *testing.T) {
	ip := newIP()
	for i := range 20 {
		if res := sendOTP(t, "phone", newPhone(), ip); res.Status != 200 {
			t.Fatalf("send %d: %d %s", i+1, res.Status, res.Body)
		}
	}
	if res := sendOTP(t, "phone", newPhone(), ip); res.Status != 429 || errorOf(t, res).Code != "otp_limit" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestOTPSendRejectsALandline(t *testing.T) {
	if res := sendOTP(t, "phone", "0201234567"); res.Status != 422 || errorOf(t, res).Code != "invalid_phone" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestOTPSendRejectsBadRequests(t *testing.T) {
	res := call(t, server, "POST", "/v1/auth/otp",
		map[string]string{"kind": "phone", "destination": newPhone(), "purpose": "add_identity"}, newIP(), newKey())
	if e := errorOf(t, res); res.Status != 400 || e.Fields["purpose"] == "" {
		t.Fatalf("unknown purpose: %d %s", res.Status, res.Body)
	}
	res = call(t, server, "POST", "/v1/auth/otp",
		map[string]string{"kind": "phone", "destination": newPhone(), "purpose": "login"}, newIP())
	if e := errorOf(t, res); res.Status != 400 || e.Fields["Idempotency-Key"] == "" {
		t.Fatalf("no Idempotency-Key: %d %s", res.Status, res.Body)
	}
}

func TestSignUpThenSignIn(t *testing.T) {
	first, phone := newAccount(t)
	if !first.Account.IsNew || first.TokenType != "Bearer" || first.AccessToken == "" || first.RefreshToken == "" {
		t.Fatalf("%+v", first)
	}
	if second := signInWith(t, phone); second.Account.ID != first.Account.ID || second.Account.IsNew {
		t.Fatalf("second %+v", second.Account)
	}
}

func TestSignInIgnoresPhoneFormat(t *testing.T) {
	phone := newPhone() // +849xxxxxxxx
	a := signInWith(t, "0"+phone[3:])
	b := signInWith(t, "+84 "+phone[3:5]+" "+phone[5:8]+" "+phone[8:])
	if a.Account.ID != b.Account.ID || !a.Account.IsNew || b.Account.IsNew {
		t.Fatalf("%+v, %+v", a.Account, b.Account)
	}
}

func TestSignInReplayedKeyGetsTheSameAccount(t *testing.T) {
	id, code := requestCode(t, newPhone())
	key := newKey()
	a := verifyCode(t, id, code, key)
	b := verifyCode(t, id, code, key)
	sa, sb := decode[signIn](t, a), decode[signIn](t, b)
	if a.Status != 200 || b.Status != 200 || sa.Account != sb.Account || sb.RefreshToken == sa.RefreshToken {
		t.Fatalf("a %d %s, b %d %s", a.Status, a.Body, b.Status, b.Body)
	}
	if res := refresh(t, sb.RefreshToken); res.Status != 200 {
		t.Fatalf("the replay's refresh token: %d %s", res.Status, res.Body)
	}
}

func TestVerifyWrongCode(t *testing.T) {
	id, code := requestCode(t, newPhone())
	res := verifyCode(t, id, wrongCode(code))
	if e := errorOf(t, res); res.Status != 422 || e.Code != "invalid_code" || e.Details["attempts_left"] != float64(4) {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestVerifyUnknownChallenge(t *testing.T) {
	if res := verifyCode(t, uuid.NewV7().String(), "123456"); res.Status != 422 || errorOf(t, res).Code != "code_expired" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestVerifyBadShape(t *testing.T) {
	if res := verifyCode(t, uuid.NewV7().String(), "12345"); res.Status != 400 || errorOf(t, res).Fields["code"] != "pattern" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestVerifySuspendedAccount(t *testing.T) {
	acc, phone := newAccount(t)
	suspend(t, acc.Account.ID)
	id, code := requestCode(t, phone)
	if res := verifyCode(t, id, code); res.Status != 403 || errorOf(t, res).Code != "account_suspended" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

// TestVerifyParallelGuesses fires 30 wrong guesses at one challenge with
// separate keys. Every reply is 422 or 409, and the challenge ends with at
// most five counted attempts.
func TestVerifyParallelGuesses(t *testing.T) {
	id, code := requestCode(t, newPhone())
	statuses := make(chan int, 30)
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() { statuses <- verifyCodeStatus(id, wrongCode(code)) })
	}
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if s != 422 && s != 409 {
			t.Fatalf("status %d", s)
		}
	}
	var attempts int
	db.QueryRow(context.Background(), `SELECT attempts FROM otp_challenges WHERE id = @id`, pgx.StrictNamedArgs{"id": id}).Scan(&attempts)
	if attempts > 5 {
		t.Fatalf("attempts %d", attempts)
	}
}

// TestOTPSendRepliesAlikeForKnownAndUnknown is the enumeration test.
func TestOTPSendRepliesAlikeForKnownAndUnknown(t *testing.T) {
	_, known := newAccount(t)
	forgetSends(t, known)
	a, b := sendOTP(t, "phone", known), sendOTP(t, "phone", newPhone())
	ka, kb := decode[map[string]any](t, a), decode[map[string]any](t, b)
	delete(ka, "challenge_id")
	delete(kb, "challenge_id")
	if a.Status != b.Status || !reflect.DeepEqual(ka, kb) {
		t.Fatalf("known %d %s, unknown %d %s", a.Status, a.Body, b.Status, b.Body)
	}
}

// TestVerifyReusedKeyWithAnotherChallengeIsRefused: a repeated
// Idempotency-Key must not hand out the first caller's session to a request
// for another challenge with a wrong code.
func TestVerifyReusedKeyWithAnotherChallengeIsRefused(t *testing.T) {
	victimID, victimCode := requestCode(t, newPhone())
	key := newKey()
	victim := decode[signIn](t, verifyCode(t, victimID, victimCode, key))
	attackerID, attackerCode := requestCode(t, newPhone())
	res := verifyCode(t, attackerID, wrongCode(attackerCode), key)
	if res.Status == 200 && decode[signIn](t, res).Account.ID == victim.Account.ID {
		t.Fatalf("another challenge with a wrong code got the victim's account: %s", res.Body)
	}
	if res.Status != 422 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

// TestVerifyReplayAfterLogoutIsRefused: replaying the verify request of a
// signed-out session must not bring the session back.
func TestVerifyReplayAfterLogoutIsRefused(t *testing.T) {
	id, code := requestCode(t, newPhone())
	key := newKey()
	first := decode[signIn](t, verifyCode(t, id, code, key))
	if res := call(t, server, "POST", "/v1/auth/logout", nil, bearer(first.AccessToken)); res.Status != 204 {
		t.Fatalf("logout %d", res.Status)
	}
	if res := verifyCode(t, id, wrongCode(code), key); res.Status != 422 {
		t.Fatalf("replay with a wrong code: %d %s", res.Status, res.Body)
	}
	if res := verifyCode(t, id, code, key); res.Status != 401 || errorOf(t, res).Code != "session_revoked" {
		t.Fatalf("replay with the right code: %d %s", res.Status, res.Body)
	}
}
