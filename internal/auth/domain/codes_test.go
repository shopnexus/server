package domain_test

import (
	"bytes"
	"regexp"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/platform/contact"
)

var key1, key2 = []byte("key-one-key-one-key-one-key-one!"), []byte("key-two-key-two-key-two-key-two!")

func TestCodeIsDerivedFromTheChallenge(t *testing.T) {
	t.Parallel()
	six := regexp.MustCompile(`^\d{6}$`)
	codes := domain.NewCodes(key1)
	id := uuid.NewV7()
	if c := codes.For(id); !six.MatchString(c) || c != codes.For(id) {
		t.Fatalf("code %q is not a stable six-digit code", c)
	}
	same := 0
	for range 100 {
		x := uuid.NewV7()
		if domain.NewCodes(key2).For(x) == codes.For(x) {
			same++
		}
	}
	if same > 2 {
		t.Fatalf("%d of 100 codes equal under two keys", same)
	}
	seen := map[string]bool{}
	for range 1000 {
		seen[codes.For(uuid.NewV7())] = true
	}
	if len(seen) < 990 {
		t.Fatalf("only %d distinct codes in 1000", len(seen))
	}
}

func TestHashBindsKeyAndDestination(t *testing.T) {
	t.Parallel()
	a, _ := contact.ParsePhone("0901234567")
	b, _ := contact.ParsePhone("0907654321")
	h1, h2 := domain.NewCodes(key1), domain.NewCodes(key2)
	if !bytes.Equal(h1.Hash(a, "123456"), h1.Hash(a, "123456")) ||
		bytes.Equal(h1.Hash(a, "123456"), h2.Hash(a, "123456")) ||
		bytes.Equal(h1.Hash(a, "123456"), h1.Hash(b, "123456")) ||
		bytes.Equal(h1.Hash(a, "123456"), h1.Hash(a, "123457")) {
		t.Fatal("hash must be stable and depend on key, destination and code")
	}
}
