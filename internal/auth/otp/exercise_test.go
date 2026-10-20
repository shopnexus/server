package otp_test

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/contact"
)

// TestSHA256CodesFallToATable is the spec's reason for HMAC: a dump of plain
// SHA-256 code hashes is reversed by hashing all 10^6 codes. The log line
// shows how long that took. The same table finds nothing in HMAC hashes,
// whose key is not in the dump.
func TestSHA256CodesFallToATable(t *testing.T) {
	dest, _ := contact.ParsePhone("0901234567")
	codes := otp.NewCodes([]byte("a-key-that-is-not-in-the-database"))
	plain, keyed := map[[32]byte]bool{}, map[[32]byte]bool{}
	for range 1000 {
		code := codes.For(uuid.NewV7())
		plain[sha256.Sum256([]byte(code))] = true
		keyed[[32]byte(codes.Hash(dest, code))] = true
	}
	start := time.Now()
	crackedPlain, crackedKeyed := 0, 0
	for n := range 1_000_000 {
		h := sha256.Sum256(fmt.Appendf(nil, "%06d", n))
		if plain[h] {
			crackedPlain++
		}
		if keyed[h] {
			crackedKeyed++
		}
	}
	t.Logf("hashed every 6-digit code in %s: %d of %d SHA-256 hashes reversed, %d of %d HMAC hashes",
		time.Since(start), crackedPlain, len(plain), crackedKeyed, len(keyed))
	if crackedPlain != len(plain) || crackedKeyed != 0 {
		t.Fatal("the table should reverse every SHA-256 hash and no HMAC hash")
	}
}
