package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"time"
)

// NewAuthKeys returns fresh random keys, the OTP key included, with no
// issuer or audience. `make run` adds them to .env; a test sets Issuer and
// Audience itself.
func NewAuthKeys() AuthConfig {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return AuthConfig{
		OTPKey:   NewConsoleKey(),
		JWTKeyID: "k" + time.Now().UTC().Format("20060102150405"),
		JWTKey:   priv,
		Issuer:   "",
		Audience: "",
	}
}

// NewConsoleKey returns a fresh CONSOLE_KEY. `make run` adds it to .env.
func NewConsoleKey() []byte {
	k := make([]byte, 32)
	rand.Read(k)
	return k
}
