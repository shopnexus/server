package main

import (
	"strings"
	"testing"
)

// The pace has no default: without -every the command stops before it
// reads any configuration.
func TestRunNeedsEvery(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"-every", "0s"}, {"-every", "soon"}} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "-every") {
			t.Errorf("run(%q) = %v, want an error naming -every", args, err)
		}
	}
}
