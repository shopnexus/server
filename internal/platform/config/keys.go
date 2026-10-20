package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

// NewAuthKeys returns fresh random keys. `make keys` prints them for .env;
// tests use them directly.
func NewAuthKeys() AuthConfig {
	otpKey := make([]byte, 32)
	rand.Read(otpKey)
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return AuthConfig{
		OTPKey:   otpKey,
		JWTKeyID: "k" + time.Now().UTC().Format("20060102150405"),
		JWTKey:   priv,
		Issuer:   "server2",
		Audience: "shopnexus-app",
	}
}

// Env renders the keys as .env lines that Load reads back.
func (a AuthConfig) Env() string {
	enc := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("OTP_KEY=%s\nJWT_KEY_ID=%s\nJWT_PRIVATE_KEY=%s\n", enc(a.OTPKey), a.JWTKeyID, enc(a.JWTKey.Seed()))
}
