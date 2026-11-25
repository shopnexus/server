package domain

import (
	"crypto/rand"
	"strings"
	"time"
)

const (
	// CodeLife is how long a code lasts.
	CodeLife = 30 * time.Minute
	// codeAlphabet leaves out 0, O, 1, I and L, which read alike.
	codeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	codeLength   = 6
)

// Code is the code a seller puts in their profile's bio.
type Code struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewCode is SN- and six random characters of the alphabet, live for
// CodeLife from now.
func NewCode(now time.Time) Code {
	b := make([]byte, codeLength)
	for i := range b {
		b[i] = codeAlphabet[randIndex(len(codeAlphabet))]
	}
	return Code{Code: "SN-" + string(b), ExpiresAt: now.Add(CodeLife)}
}

// randIndex is a uniform index below n from crypto/rand.
func randIndex(n int) int {
	limit := 256 - 256%n
	var b [1]byte
	for {
		_, _ = rand.Read(b[:])
		if int(b[0]) < limit {
			return int(b[0]) % n
		}
	}
}

// Live reports a code before its expiry.
func (c Code) Live(now time.Time) bool { return now.Before(c.ExpiresAt) }

// Holds reports whether text holds code, case, spaces and dashes aside, so
// "sn 7k3q9m" and "SN–7K3Q9M" count.
func Holds(text, code string) bool {
	c := squeeze(code)
	return c != "" && strings.Contains(squeeze(text), c)
}

// squeeze is s in upper case without white space or dashes.
func squeeze(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case ' ', '\t', '\n', '\r', ' ', '-', '‐', '‑', '–', '—', '_':
			continue
		default:
		}
		b.WriteRune(r)
	}
	return b.String()
}
