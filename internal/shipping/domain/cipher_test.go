package domain_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/shipping/domain"
)

func key(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpens(t *testing.T) {
	t.Parallel()
	k := key(t)
	sealed, nonce, err := domain.Seal(k, "shop-token-123")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("shop-token-123")) {
		t.Fatal("the sealed token holds the token")
	}
	again, nonce2, _ := domain.Seal(k, "shop-token-123")
	if bytes.Equal(again, sealed) || bytes.Equal(nonce, nonce2) {
		t.Fatal("two seals of one token are the same")
	}
	if got, err := domain.Open(k, sealed, nonce); err != nil || got != "shop-token-123" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestAWrongKeyDoesNotOpenTheToken(t *testing.T) {
	t.Parallel()
	sealed, nonce, err := domain.Seal(key(t), "shop-token-123")
	if err != nil {
		t.Fatal(err)
	}
	_, err = domain.Open(key(t), sealed, nonce)
	if !errors.Is(err, domain.ErrSealedToken) || strings.Contains(err.Error(), "shop-token-123") {
		t.Fatalf("%v", err)
	}
}
