package e2e

import (
	"encoding/json"
	"testing"
	"uuid"
)

// The helpers in this file serve every module's tests; auth_helpers_test.go
// holds the OTP and sign-in steps.

// decode fails the test unless the response body is JSON for a T.
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

// errorOf decodes the shared error format.
func errorOf(t *testing.T, res Response) apiError {
	t.Helper()
	return decode[struct {
		Error apiError `json:"error"`
	}](t, res).Error
}

// bearer returns an Authorization header for an access token.
func bearer(token string) [2]string { return [2]string{"Authorization", "Bearer " + token} }

// newKey returns a fresh Idempotency-Key header.
func newKey() [2]string { return [2]string{"Idempotency-Key", uuid.NewV7().String()} }
