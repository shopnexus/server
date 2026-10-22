package restatex

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mr-tron/base58"
)

// The Restate server signs each request to an endpoint with the Ed25519 key
// in its worker.invoker.request-identity-private-key-pem-file setting. An
// endpoint given the public half runs only signed requests, so a caller that
// reaches its port without the Restate server cannot invoke a handler.

// EnsureIdentity returns the public key of the PKCS#8 PEM Ed25519 key at
// path, in the form Restate prints ("publickeyv1_" and base58). When no file
// is there it writes a new key first.
func EnsureIdentity(path string) (string, error) {
	key, err := IdentityKey(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return key, err
	}
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return "", err
	}
	return IdentityKey(path)
}

// IdentityKey returns the public key of the PEM Ed25519 key at path.
func IdentityKey(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return "", fmt.Errorf("%s: no PEM block", path)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return "", fmt.Errorf("%s: not an Ed25519 key", path)
	}
	return "publickeyv1_" + base58.Encode(priv.Public().(ed25519.PublicKey)), nil
}
