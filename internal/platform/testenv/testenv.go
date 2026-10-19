// Package testenv gives tests the address of the services they need. Under
// `go test -short` such tests skip; without -short a missing service fails
// them, so a broken setup is never mistaken for a pass.
package testenv

import (
	"os"
	"testing"
)

// DatabaseURL returns TEST_DATABASE_URL, the admin connection to the test
// stack's Postgres.
func DatabaseURL(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Postgres; skipped under -short")
	}
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is not set: run `make test-up` and use `make test-int`")
	}
	return url
}
