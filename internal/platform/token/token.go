// Package token signs and verifies the EdDSA access tokens of every module
// and makes the random refresh tokens they rotate. The caller names the
// audience on both sides, so one module's tokens fail in another's verifier.
package token

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/config"
)

// ErrInvalid is every verification failure; the cause is kept for the log.
var ErrInvalid = apperr.New(apperr.KindUnauthenticated, "invalid_token", "the access token is not valid")

// Claims is what a token says. SessionID, AuthTime and Version are zero
// when a token does not carry them.
type Claims struct {
	Subject   uuid.UUID
	SessionID uuid.UUID
	AuthTime  time.Time
	Version   int // the staff credentials version a login ticket was issued at
	Audience  string
	ExpiresAt time.Time // set by Verify
}

// jwtClaims is the wire form. No phone, email or name goes in: anyone
// holding a token can read it.
type jwtClaims struct {
	jwt.RegisteredClaims

	SessionID string `json:"sid,omitempty"`
	AuthTime  int64  `json:"auth_time,omitempty"`
	Version   int    `json:"cv,omitempty"`
}

// Signer signs with the current Ed25519 key.
type Signer struct {
	kid    string
	key    ed25519.PrivateKey
	issuer string
}

func NewSigner(cfg config.AuthConfig) Signer {
	return Signer{kid: cfg.JWTKeyID, key: cfg.JWTKey, issuer: cfg.Issuer}
}

// Sign returns a token for c valid ttl from now, and its expiry.
func (s Signer) Sign(c Claims, ttl time.Duration, now time.Time) (string, time.Time, error) {
	exp := now.Add(ttl)
	wire := jwtClaims{
		Issuer: s.issuer, Audience: jwt.ClaimStrings{c.Audience}, Subject: c.Subject.String(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp), NotBefore: nil, ID: "",
		SessionID: "", AuthTime: 0, Version: c.Version,
	}
	if c.SessionID != (uuid.UUID{}) {
		wire.SessionID = c.SessionID.String()
	}
	if !c.AuthTime.IsZero() {
		wire.AuthTime = c.AuthTime.Unix()
	}
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, wire)
	t.Header["kid"] = s.kid
	signed, err := t.SignedString(s.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Verifier checks tokens against the current key and, during a key change,
// the previous one.
type Verifier struct {
	keys   map[string]ed25519.PublicKey
	issuer string
}

func NewVerifier(cfg config.AuthConfig) Verifier {
	keys := map[string]ed25519.PublicKey{cfg.JWTKeyID: cfg.JWTKey.Public().(ed25519.PublicKey)} //nolint:errcheck // An ed25519.PrivateKey always returns an ed25519.PublicKey.
	if cfg.JWTPrevKeyID != "" {
		keys[cfg.JWTPrevKeyID] = cfg.JWTPrevKey
	}
	return Verifier{keys: keys, issuer: cfg.Issuer}
}

// Verify returns the claims of a token valid at now for audience. Only
// EdDSA is accepted, which closes alg:none and algorithm-swap attacks.
func (v Verifier) Verify(tok, audience string, now time.Time) (Claims, error) {
	var c jwtClaims
	_, err := jwt.ParseWithClaims(tok, &c, func(t *jwt.Token) (any, error) {
		kid, ok := t.Header["kid"].(string)
		if !ok {
			return nil, errors.New("no kid header")
		}
		key, ok := v.keys[kid]
		if !ok {
			return nil, fmt.Errorf("unknown kid %q", kid)
		}
		return key, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(audience),
		jwt.WithLeeway(30*time.Second),
		jwt.WithTimeFunc(func() time.Time { return now }),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return Claims{}, invalid(err)
	}
	sub, err := uuid.Parse(c.Subject)
	if err != nil {
		return Claims{}, invalid(errors.New("sub is not a UUID"))
	}
	out := Claims{Subject: sub, SessionID: uuid.UUID{}, AuthTime: time.Time{}, Version: c.Version, Audience: audience, ExpiresAt: c.ExpiresAt.UTC()}
	if c.SessionID != "" {
		if out.SessionID, err = uuid.Parse(c.SessionID); err != nil {
			return Claims{}, invalid(errors.New("sid is not a UUID"))
		}
	}
	if c.AuthTime != 0 {
		out.AuthTime = time.Unix(c.AuthTime, 0).UTC()
	}
	return out, nil
}

func invalid(cause error) error {
	e := *ErrInvalid
	e.Err = cause
	return &e
}

// NewRefresh returns rt_ plus 32 random bytes in base64url, and its hash.
func NewRefresh() (token string, hash []byte) {
	b := make([]byte, 32)
	rand.Read(b)
	token = "rt_" + base64.RawURLEncoding.EncodeToString(b)
	return token, HashRefresh(token)
}

// HashRefresh is plain SHA-256: the token carries 256 random bits, so a slow
// hash would add nothing.
func HashRefresh(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
