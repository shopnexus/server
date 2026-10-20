package otp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/contact"
)

// Codes derives and hashes OTP codes with OTP_KEY. A code is derived from its
// challenge ID, so a retried send step produces the same code and the code
// is never stored or written to Restate's journal.
type Codes struct{ key []byte }

func NewCodes(key []byte) Codes { return Codes{key: key} }

// For returns the six-digit code of a challenge. Taking 64 bits modulo 10^6
// leaves a bias of under 10^-13.
func (c Codes) For(challengeID uuid.UUID) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte("otp-code:"))
	m.Write(challengeID[:])
	return fmt.Sprintf("%06d", binary.BigEndian.Uint64(m.Sum(nil)[:8])%1_000_000)
}

// Hash is what otp_challenges.code_hash holds. Binding the destination makes
// equal codes for different people hash differently.
func (c Codes) Hash(dest contact.Address, code string) []byte {
	m := hmac.New(sha256.New, c.key)
	fmt.Fprintf(m, "otp-hash:%s:%s:%s", dest.Kind(), dest.Value(), code)
	return m.Sum(nil)
}
