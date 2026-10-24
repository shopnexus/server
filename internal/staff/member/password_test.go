package member_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/staff/member"
)

func TestHashAndVerify(t *testing.T) {
	t.Parallel()
	h := member.Fast.Hash("correct horse battery")
	if !strings.HasPrefix(h, "$argon2id$v=19$m=8,t=1,p=1$") {
		t.Fatalf("PHC string: %s", h)
	}
	if !member.VerifyPassword(h, "correct horse battery") || member.VerifyPassword(h, "correct horse batterz") {
		t.Fatal("verify")
	}
	first, second := member.Fast.Hash("same"), member.Fast.Hash("same")
	if first == second {
		t.Fatal("two hashes of one password share a salt")
	}
	other := member.Hasher{MemoryKiB: 16, Time: 2, Threads: 2}.Hash("p")
	if !member.VerifyPassword(other, "p") {
		t.Fatal("parameters come from the hash")
	}
	for _, bad := range []string{"", "plain", "$argon2id$v=19$m=8,t=1,p=1$!!$!!", "$argon2i$v=19$m=8,t=1,p=1$AAAA$AAAA"} {
		if member.VerifyPassword(bad, "p") {
			t.Errorf("verified against %q", bad)
		}
	}
}

func TestCheckPolicy(t *testing.T) {
	t.Parallel()
	for _, weak := range []string{"short-pass", strings.Repeat("x", 129), "Mod@Example.com"} {
		if err := member.CheckPolicy(weak, "mod@example.com"); !errors.Is(err, member.ErrPasswordTooWeak) {
			t.Errorf("%q: %v", weak, err)
		}
	}
	if err := member.CheckPolicy("twelve chars", "mod@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := member.CheckPolicy(strings.Repeat("ữ", 12), "mod@example.com"); err != nil {
		t.Fatalf("characters, not bytes: %v", err)
	}
}

func TestTemporaryPassword(t *testing.T) {
	t.Parallel()
	a, b := member.TemporaryPassword(), member.TemporaryPassword()
	if len(a) != 16 || a == b || member.CheckPolicy(a, "x@y.z") != nil {
		t.Fatalf("%q %q", a, b)
	}
}
