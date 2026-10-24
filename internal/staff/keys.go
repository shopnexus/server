package staff

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
)

const (
	AudienceStaff = "shopnexus-staff"
	AudienceLogin = "shopnexus-staff-login"
	TicketTTL     = 5 * time.Minute
)

// Hasher is member.Hasher, so app and tests pick one without importing
// package member.
type Hasher = member.Hasher

var (
	ProductionHasher = member.Production
	FastHasher       = member.Fast
)

// Keys is what staff signs, seals and hashes with.
type Keys struct {
	Secrets  cipher.AEAD // seals TOTP secrets at rest
	Quota    []byte      // the HMAC key of login quota keys
	Hasher   Hasher
	Signer   token.Signer
	Verifier token.Verifier
	dummy    string // a hash no password matches, verified for unknown emails
}

// NewKeys derives the TOTP sealing key and the quota HMAC key from
// STAFF_KEY with HKDF, under different labels, and signs with the auth JWT
// key under the staff audiences.
func NewKeys(staffCfg config.StaffConfig, authCfg config.AuthConfig, h Hasher) (Keys, error) {
	sealKey, err := hkdf.Key(sha256.New, staffCfg.Key, nil, "shopnexus staff totp secrets", 32)
	if err != nil {
		return Keys{}, fmt.Errorf("derive the totp key: %w", err)
	}
	quotaKey, err := hkdf.Key(sha256.New, staffCfg.Key, nil, "shopnexus staff login quota", 32)
	if err != nil {
		return Keys{}, fmt.Errorf("derive the quota key: %w", err)
	}
	block, err := aes.NewCipher(sealKey)
	if err != nil {
		return Keys{}, fmt.Errorf("totp cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Keys{}, fmt.Errorf("totp cipher: %w", err)
	}
	return Keys{
		Secrets: aead, Quota: quotaKey, Hasher: h,
		Signer: token.NewSigner(authCfg), Verifier: token.NewVerifier(authCfg), dummy: h.Hash(rand.Text()),
	}, nil
}
