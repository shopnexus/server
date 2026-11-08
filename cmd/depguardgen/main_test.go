package main

import (
	"bytes"
	"testing"
)

// The test makes go test, and so make test, fail when a module or a layer
// package was added without make depguard.
func TestConfigIsCurrent(t *testing.T) {
	t.Parallel()
	current, want, err := generate("../..")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, want) {
		t.Fatal("the depguard rules in .golangci.yml are stale; run make depguard")
	}
}
