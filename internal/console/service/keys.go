package service

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/secret"
)

// Keys is what module console seals and hashes with.
type Keys struct {
	Secrets *secret.Box // seals TOTP secrets at rest
	Locks   []byte      // the HMAC key of sign-in lock keys
	Hasher  domain.Hasher
	dummy   string // a hash no password matches, verified for unknown emails
}

// NewKeys derives the TOTP sealing key and the lock HMAC key from CONSOLE_KEY
// with HKDF, under different labels.
func NewKeys(cfg config.ConsoleConfig, h domain.Hasher) (Keys, error) {
	sealKey, err := hkdf.Key(sha256.New, cfg.Key, nil, "shopnexus console totp secrets", 32)
	if err != nil {
		return Keys{}, fmt.Errorf("derive the totp key: %w", err)
	}
	lockKey, err := hkdf.Key(sha256.New, cfg.Key, nil, "shopnexus console login lock", 32)
	if err != nil {
		return Keys{}, fmt.Errorf("derive the lock key: %w", err)
	}
	box, err := secret.New(sealKey)
	if err != nil {
		return Keys{}, fmt.Errorf("totp cipher: %w", err)
	}
	return Keys{Secrets: box, Locks: lockKey, Hasher: h, dummy: h.Hash(rand.Text())}, nil
}
