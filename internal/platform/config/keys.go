package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

// NewAuthKeys returns fresh random keys with no issuer or audience. `make run`
// adds them to .env; a test sets Issuer and Audience itself.
func NewAuthKeys() AuthConfig {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return AuthConfig{
		JWTKeyID: "k" + time.Now().UTC().Format("20060102150405"),
		JWTKey:   priv,
		Issuer:   "",
		Audience: "",
	}
}

// Env renders the keys as .env lines that Load reads back.
func (a AuthConfig) Env() string {
	enc := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("JWT_KEY_ID=%s\nJWT_PRIVATE_KEY=%s\n", a.JWTKeyID, enc(a.JWTKey.Seed()))
}

// NewStaffKey returns a fresh STAFF_KEY. `make run` adds it to .env.
func NewStaffKey() []byte {
	k := make([]byte, 32)
	rand.Read(k)
	return k
}
