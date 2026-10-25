package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/staff/domain"
)

func TestHashAndVerify(t *testing.T) {
	t.Parallel()
	h := domain.Fast.Hash("correct horse battery")
	if !strings.HasPrefix(h, "$argon2id$v=19$m=8,t=1,p=1$") {
		t.Fatalf("PHC string: %s", h)
	}
	if !domain.VerifyPassword(h, "correct horse battery") || domain.VerifyPassword(h, "correct horse batterz") {
		t.Fatal("verify")
	}
	first, second := domain.Fast.Hash("same"), domain.Fast.Hash("same")
	if first == second {
		t.Fatal("two hashes of one password share a salt")
	}
	other := domain.Hasher{MemoryKiB: 16, Time: 2, Threads: 2}.Hash("p")
	if !domain.VerifyPassword(other, "p") {
		t.Fatal("parameters come from the hash")
	}
	for _, bad := range []string{"", "plain", "$argon2id$v=19$m=8,t=1,p=1$!!$!!", "$argon2i$v=19$m=8,t=1,p=1$AAAA$AAAA"} {
		if domain.VerifyPassword(bad, "p") {
			t.Errorf("verified against %q", bad)
		}
	}
}

func TestCheckPolicy(t *testing.T) {
	t.Parallel()
	for _, weak := range []string{"short-pass", strings.Repeat("x", 129), "Mod@Example.com"} {
		if err := domain.CheckPolicy(weak, "mod@example.com"); !errors.Is(err, domain.ErrPasswordTooWeak) {
			t.Errorf("%q: %v", weak, err)
		}
	}
	if err := domain.CheckPolicy("twelve chars", "mod@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := domain.CheckPolicy(strings.Repeat("ữ", 12), "mod@example.com"); err != nil {
		t.Fatalf("characters, not bytes: %v", err)
	}
}

func TestTemporaryPassword(t *testing.T) {
	t.Parallel()
	a, b := domain.TemporaryPassword(), domain.TemporaryPassword()
	if len(a) != 16 || a == b || domain.CheckPolicy(a, "x@y.z") != nil {
		t.Fatalf("%q %q", a, b)
	}
}
