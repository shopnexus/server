package e2e

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"
	"uuid"
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
