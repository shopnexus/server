package staff

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/staff/member"
)

// Keys is what staff seals and hashes with.
type Keys struct {
	Secrets cipher.AEAD // seals TOTP secrets at rest
	Locks   []byte      // the HMAC key of sign-in lock keys
	Hasher  member.Hasher
	dummy   string // a hash no password matches, verified for unknown emails
}

// NewKeys derives the TOTP sealing key and the lock HMAC key from STAFF_KEY
// with HKDF, under different labels.
func NewKeys(cfg config.StaffConfig, h member.Hasher) (Keys, error) {
	sealKey, err := hkdf.Key(sha256.New, cfg.Key, nil, "shopnexus staff totp secrets", 32)
	if err != nil {
		return Keys{}, fmt.Errorf("derive the totp key: %w", err)
	}
	lockKey, err := hkdf.Key(sha256.New, cfg.Key, nil, "shopnexus staff login lock", 32)
	if err != nil {
		return Keys{}, fmt.Errorf("derive the lock key: %w", err)
	}
	block, err := aes.NewCipher(sealKey)
	if err != nil {
		return Keys{}, fmt.Errorf("totp cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Keys{}, fmt.Errorf("totp cipher: %w", err)
	}
	return Keys{Secrets: aead, Locks: lockKey, Hasher: h, dummy: h.Hash(rand.Text())}, nil
}
