package pgtest

import (
	"strings"
	"testing"
)

func TestGuardRejectsNonTestDatabases(t *testing.T) {
	t.Parallel()
	cases := []string{
		"postgres://server:server@localhost:56432/server?sslmode=disable",     // dev stack
		"postgres://server:server@localhost:56433/server?sslmode=disable",     // test port, dev name
		"postgres://server:server@localhost:5432/server_test?sslmode=disable", // test name, other port
	}
	for _, url := range cases {
		if err := guard(url); err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("guard(%q) = %v", url, err)
		}
	}
	if err := guard("postgres://server:server@localhost:56433/server_test?sslmode=disable"); err != nil {
		t.Errorf("test stack rejected: %v", err)
	}
}
