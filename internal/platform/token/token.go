// Package token makes random opaque tokens, such as refresh tokens and
// session cookies, that a store keeps only as a hash.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// NewOpaque returns prefix plus 32 random bytes in base64url, and its hash.
// The prefix says what the token is, such as rt_ for a refresh token.
func NewOpaque(prefix string) (token string, hash []byte) {
	b := make([]byte, 32)
	rand.Read(b)
	token = prefix + base64.RawURLEncoding.EncodeToString(b)
	return token, Hash(token)
}

// Hash is plain SHA-256: an opaque token carries 256 random bits, so a slow
// hash would add nothing.
func Hash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
