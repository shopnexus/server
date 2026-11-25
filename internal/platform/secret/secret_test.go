package secret_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/shopnexus/server/internal/platform/secret"
)

func box(t *testing.T) *secret.Box {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	b, err := secret.New(k)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealOpens(t *testing.T) {
	t.Parallel()
	b := box(t)
	sealed := b.Seal([]byte("shop-token-123"), []byte("row-1"))
	if bytes.Contains(sealed, []byte("shop-token-123")) {
		t.Fatal("the sealed value holds the plain one")
	}
	if again := b.Seal([]byte("shop-token-123"), []byte("row-1")); bytes.Equal(again, sealed) {
		t.Fatal("two seals of one value are the same")
	}
	if got, err := b.Open(sealed, []byte("row-1")); err != nil || string(got) != "shop-token-123" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestAWrongKeyOrRowDoesNotOpen(t *testing.T) {
	t.Parallel()
	b := box(t)
	sealed := b.Seal([]byte("shop-token-123"), []byte("row-1"))
	for name, open := range map[string]func() ([]byte, error){
		"key":   func() ([]byte, error) { return box(t).Open(sealed, []byte("row-1")) },
		"row":   func() ([]byte, error) { return b.Open(sealed, []byte("row-2")) },
		"short": func() ([]byte, error) { return b.Open(sealed[:3], []byte("row-1")) },
	} {
		if _, err := open(); !errors.Is(err, secret.ErrOpen) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
