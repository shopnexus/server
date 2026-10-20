package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/contact"
)

// newPhone returns a fresh mobile number, so tests never share a
// destination's quota.
func newPhone() string { return fmt.Sprintf("+849%08d", rand.IntN(100_000_000)) }

func newEmail() string { return fmt.Sprintf("e2e-%d@example.com", rand.Int64()) }

// newIP returns an X-Forwarded-For header with a fresh address, so tests
// never share an address's quota.
func newIP() [2]string {
	return [2]string{"X-Forwarded-For", fmt.Sprintf("198.18.%d.%d", rand.IntN(256), rand.IntN(256))}
}

// newKey returns a fresh Idempotency-Key header.
func newKey() [2]string { return [2]string{"Idempotency-Key", uuid.NewV7().String()} }

func decode[T any](t *testing.T, res Response) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(res.Body, &v); err != nil {
		t.Fatalf("decode %s: %v", res.Body, err)
	}
	return v
}

type apiError struct {
	Code    string            `json:"code"`
	Fields  map[string]string `json:"fields"`
	Details map[string]any    `json:"details"`
}

func errorOf(t *testing.T, res Response) apiError {
	t.Helper()
	return decode[struct {
		Error apiError `json:"error"`
	}](t, res).Error
}

type challenge struct {
	ChallengeID string `json:"challenge_id"`
	ResendAfter int    `json:"resend_after"`
}

// sendOTP sends with a fresh address and key unless headers override them.
func sendOTP(t *testing.T, kind, dest string, headers ...[2]string) Response {
	t.Helper()
	return call(t, server, "POST", "/v1/auth/otp",
		map[string]string{"kind": kind, "destination": dest, "purpose": "login"},
		append([][2]string{newIP(), newKey()}, headers...)...)
}

type signIn struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Account      struct {
		ID    string `json:"id"`
		IsNew bool   `json:"is_new"`
	} `json:"account"`
}

func bearer(token string) [2]string { return [2]string{"Authorization", "Bearer " + token} }

func wrongCode(code string) string {
	if code == "000000" {
		return "000001"
	}
	return "000000"
}

// requestCode sends a code to a phone typed any way and returns the challenge
// and the code. Earlier sends to that phone are pushed back a minute in its
// quota, so a test can ask again without waiting 45 s.
func requestCode(t *testing.T, typed string) (string, string) {
	t.Helper()
	p, err := contact.ParsePhone(typed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), `
UPDATE otp_quotas SET sent_at = ARRAY(SELECT s - interval '1 minute' FROM unnest(sent_at) s)
WHERE key = $1`, "dest:"+p.Value()); err != nil {
		t.Fatal(err)
	}
	n := box.count(p.Value())
	res := sendOTP(t, "phone", typed)
	if res.Status != 200 {
		t.Fatalf("send: %d %s", res.Status, res.Body)
	}
	return decode[challenge](t, res).ChallengeID, box.code(t, p.Value(), n+1)
}

func verifyCode(t *testing.T, challengeID, code string, headers ...[2]string) Response {
	t.Helper()
	return call(t, server, "POST", "/v1/auth/otp/verify",
		map[string]string{"challenge_id": challengeID, "code": code, "device_name": "e2e"},
		append([][2]string{newKey()}, headers...)...)
}

// verifyCodeStatus posts one guess and returns the status without touching t,
// so goroutines can call it. It records the status for the coverage check as
// call does, and skips response validation, so its caller checks statuses.
func verifyCodeStatus(challengeID, code string) int {
	body, _ := json.Marshal(map[string]string{"challenge_id": challengeID, "code": code})
	req, _ := http.NewRequest("POST", server+"/v1/auth/otp/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uuid.NewV7().String())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	res.Body.Close()
	mu.Lock()
	if seen["verifyOtp"] == nil {
		seen["verifyOtp"] = map[int]bool{}
	}
	seen["verifyOtp"][res.StatusCode] = true
	mu.Unlock()
	return res.StatusCode
}

func signInWith(t *testing.T, typed string) signIn {
	t.Helper()
	id, code := requestCode(t, typed)
	res := verifyCode(t, id, code)
	if res.Status != 200 {
		t.Fatalf("verify: %d %s", res.Status, res.Body)
	}
	return decode[signIn](t, res)
}

func newAccount(t *testing.T) (signIn, string) {
	t.Helper()
	phone := newPhone()
	return signInWith(t, phone), phone
}
