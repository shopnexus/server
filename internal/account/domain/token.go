package domain

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/config"
)

// Tokens is what a sign-in or a refresh returns.
type Tokens struct {
	AccessToken          string    `json:"access_token"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
	RefreshToken         string    `json:"refresh_token"`
	TokenType            string    `json:"token_type"`
}

// accessClaims is an access token's payload. No phone, email or name goes
// in: anyone holding a token can read it.
type accessClaims struct {
	jwt.RegisteredClaims

	SessionID string `json:"sid"`
	AuthTime  int64  `json:"auth_time"`
}

// Signer signs customer access tokens, EdDSA with the current key.
type Signer struct {
	kid      string
	key      ed25519.PrivateKey
	issuer   string
	audience string
}

func NewSigner(cfg config.AuthConfig) Signer {
	return Signer{kid: cfg.JWTKeyID, key: cfg.JWTKey, issuer: cfg.Issuer, audience: cfg.Audience}
}

// Tokens signs an access token for x and pairs it with refresh, x's current
// refresh token.
func (s Signer) Tokens(x Session, refresh string, now time.Time) (Tokens, error) {
	access, exp, err := s.Sign(x, now)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, AccessTokenExpiresAt: exp, RefreshToken: refresh, TokenType: "Bearer"}, nil
}

// Sign returns an access token for x valid AccessTTL from now, and its expiry.
func (s Signer) Sign(x Session, now time.Time) (string, time.Time, error) {
	exp := now.Add(AccessTTL)
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, accessClaims{
		Issuer: s.issuer, Audience: jwt.ClaimStrings{s.audience}, Subject: x.accountID.String(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp),
		SessionID: x.id.String(), AuthTime: x.authTime.Unix(),
	})
	t.Header["kid"] = s.kid
	signed, err := t.SignedString(s.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, exp, nil
}

// Verifier checks customer access tokens against the current key and,
// during a key change, the previous one.
type Verifier struct {
	keys     map[string]ed25519.PublicKey
	issuer   string
	audience string
}

func NewVerifier(cfg config.AuthConfig) Verifier {
	keys := map[string]ed25519.PublicKey{cfg.JWTKeyID: cfg.JWTKey.Public().(ed25519.PublicKey)} //nolint:errcheck // An ed25519.PrivateKey always returns an ed25519.PublicKey.
	if cfg.JWTPrevKeyID != "" {
		keys[cfg.JWTPrevKeyID] = cfg.JWTPrevKey
	}
	return Verifier{keys: keys, issuer: cfg.Issuer, audience: cfg.Audience}
}

// Verify returns the principal of a token valid at now. Only EdDSA is
// accepted, which closes alg:none and algorithm-swap attacks. Every failure
// is ErrInvalidToken, with the cause kept for the log.
func (v Verifier) Verify(tok string, now time.Time) (authn.Principal, error) {
	var c accessClaims
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
		jwt.WithAudience(v.audience),
		jwt.WithLeeway(30*time.Second),
		jwt.WithTimeFunc(func() time.Time { return now }),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return authn.Principal{}, ErrInvalidToken.WithCause(err)
	}
	sub, err := uuid.Parse(c.Subject)
	if err != nil {
		return authn.Principal{}, ErrInvalidToken.WithCause(errors.New("sub is not a UUID"))
	}
	sid, err := uuid.Parse(c.SessionID)
	if err != nil {
		return authn.Principal{}, ErrInvalidToken.WithCause(errors.New("sid missing or not a UUID"))
	}
	if c.AuthTime == 0 {
		return authn.Principal{}, ErrInvalidToken.WithCause(errors.New("auth_time missing"))
	}
	return authn.Principal{AccountID: sub, SessionID: sid, AuthTime: time.Unix(c.AuthTime, 0).UTC()}, nil
}
