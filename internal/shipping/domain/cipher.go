// Package domain holds module shipping's rules: how a carrier token is
// sealed, and the errors the module answers with.
package domain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// ErrSealedToken is a token that does not open under the key: another
// environment's key, or a damaged row.
var ErrSealedToken = errors.New("shipping: the token does not open under this key")

// Seal encrypts a carrier token with AES-GCM under key, 32 bytes, with a
// fresh nonce.
func Seal(key []byte, token string) (sealed, nonce []byte, err error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("nonce: %w", err)
	}
	return gcm.Seal(nil, nonce, []byte(token), nil), nonce, nil
}

// Open decrypts a sealed token; ErrSealedToken when it does not open.
func Open(key, sealed, nonce []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", ErrSealedToken
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("token cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("token cipher: %w", err)
	}
	return gcm, nil
}
