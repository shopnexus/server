// Package secret seals secrets kept at rest, such as carrier tokens and
// TOTP secrets, with AES-256-GCM.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// ErrOpen is a sealed value that does not open under the key with its
// associated data: another environment's key, another row's data, or a
// damaged value.
var ErrOpen = errors.New("secret: the value does not open under this key")

// Box seals and opens values under one key. A sealed value is its fresh
// nonce followed by the ciphertext; aad, such as the row's ID, binds it to
// where it is stored.
type Box struct{ aead cipher.AEAD }

// New returns the Box of key, 32 bytes.
func New(key []byte) (*Box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret cipher: %w", err)
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Seal(plain, aad []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize(), b.aead.NonceSize()+len(plain)+b.aead.Overhead())
	rand.Read(nonce)
	return b.aead.Seal(nonce, nonce, plain, aad)
}

// Open returns the plain value of sealed, or ErrOpen.
func (b *Box) Open(sealed, aad []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrOpen
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}
