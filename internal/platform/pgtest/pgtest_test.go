package pgtest

import (
	"strings"
	"testing"
)

func TestGuardRejectsNonTestDatabases(t *testing.T) {
	t.Parallel()
	cases := []string{
		"postgres://server2:server2@localhost:56432/server2?sslmode=disable",     // dev stack
		"postgres://server2:server2@localhost:56433/server2?sslmode=disable",     // test port, dev name
		"postgres://server2:server2@localhost:5432/server2_test?sslmode=disable", // test name, other port
	}
	for _, url := range cases {
		if err := guard(url); err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("guard(%q) = %v", url, err)
		}
	}
	if err := guard("postgres://server2:server2@localhost:56433/server2_test?sslmode=disable"); err != nil {
		t.Errorf("test stack rejected: %v", err)
	}
}
