package seedpack_test

import (
	"testing"
	"time"

	"github.com/shopnexus/server/internal/seedpack"
)

func TestID(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 2, 10, 14, 0, 0, time.UTC)
	a, b := seedpack.ID("demo", "listing", "p-1", at), seedpack.ID("demo", "listing", "p-1", at)
	if a != b {
		t.Fatalf("%s and %s differ", a, b)
	}
	if a == seedpack.ID("demo", "listing", "p-2", at) || a == seedpack.ID("other", "listing", "p-1", at) {
		t.Fatal("different keys or packs share an ID")
	}
	if a.String()[14] != '7' {
		t.Fatalf("%s is not version 7", a)
	}
	if later := seedpack.ID("demo", "listing", "p-0", at.Add(time.Second)); later.Compare(a) <= 0 {
		t.Fatal("a later row sorts before an earlier one")
	}
}
