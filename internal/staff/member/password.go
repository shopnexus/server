package member

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Hasher holds the argon2id parameters for new hashes. VerifyPassword reads
// the parameters from the hash, so changing them never breaks a stored one.
type Hasher struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

var (
	// Production is the parameters of every stored hash outside tests.
	Production = Hasher{MemoryKiB: 64 * 1024, Time: 3, Threads: 4}
	// Fast keeps tests quick; argon2 needs at least 8 KiB per thread.
	Fast = Hasher{MemoryKiB: 8, Time: 1, Threads: 1}
)

const (
	saltLen = 16
	keyLen  = 32
	minLen  = 12
	maxLen  = 128
)

var b64 = base64.RawStdEncoding

// Hash returns password as a PHC string with a fresh salt.
func (h Hasher) Hash(password string) string {
	salt := make([]byte, saltLen)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, h.Time, h.MemoryKiB, h.Threads, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, h.MemoryKiB, h.Time, h.Threads,
		b64.EncodeToString(salt), b64.EncodeToString(key))
}

// VerifyPassword reports whether password matches phc, comparing in
// constant time. A malformed hash matches nothing.
func VerifyPassword(phc, password string) bool {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return false
	}
	var h Hasher
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &h.MemoryKiB, &h.Time, &h.Threads); err != nil {
		return false
	}
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 || h.Threads == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, h.Time, h.MemoryKiB, h.Threads, uint32(len(want))) //nolint:gosec // len(want) is a decoded hash of a few dozen bytes.
	return subtle.ConstantTimeCompare(got, want) == 1
}

// CheckPolicy applies NIST SP 800-63B: 12 to 128 characters and not the
// email, with no composition rules.
func CheckPolicy(password, email string) error {
	n := utf8.RuneCountInString(password)
	if n < minLen || n > maxLen || strings.EqualFold(password, email) {
		return ErrPasswordTooWeak
	}
	return nil
}

// TemporaryPassword is 16 characters of base32, 80 random bits.
func TemporaryPassword() string { return rand.Text()[:16] }
