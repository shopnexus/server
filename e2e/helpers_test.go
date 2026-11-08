package e2e

import (
	"encoding/json"
	"testing"
	"uuid"
)

// The helpers in this file serve every module's tests; auth_helpers_test.go
// holds the sign-in steps.

// page is one page of a list endpoint.
type page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

// decode fails the test unless the response body is JSON for a T.
func decode[T any](t *testing.T, res Response) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(res.Body, &v); err != nil {
		t.Fatalf("decode %s: %v", res.Body, err)
	}
	return v
}

// expect fails the test unless res has status and returns the body decoded
// as a T.
func expect[T any](t *testing.T, res Response, status int) T {
	t.Helper()
	expectStatus(t, res, status)
	return decode[T](t, res)
}

// expectStatus fails the test unless res has status.
func expectStatus(t *testing.T, res Response, status int) {
	t.Helper()
	if res.Status != status {
		t.Fatalf("%s %s: %d, want %d: %s", res.Method, res.Path, res.Status, status, res.Body)
	}
}

// check marks the test failed and goes on unless res has status and, when
// code is set, the error code.
func check(t *testing.T, name string, res Response, status int, code string) {
	t.Helper()
	if res.Status != status || (code != "" && errorOf(t, res).Code != code) {
		t.Errorf("%s: %d %s", name, res.Status, res.Body)
	}
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

// bearer returns an Authorization header for an access token; for no token
// it returns no header, for a call signed out.
func bearer(token string) [2]string {
	if token == "" {
		return [2]string{}
	}
	return [2]string{"Authorization", "Bearer " + token}
}

// newKey returns a fresh Idempotency-Key header.
func newKey() [2]string { return [2]string{"Idempotency-Key", uuid.NewV7().String()} }
