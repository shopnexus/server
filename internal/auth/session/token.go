package session

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

// Claims are the access token's claims. No phone, email or name goes in:
// anyone holding the token can read it.
type Claims struct {
	jwt.RegisteredClaims
	SessionID string `json:"sid"`
	AuthTime  int64  `json:"auth_time"`
}

// Tokens is what a sign-in or a refresh returns.
type Tokens struct {
	AccessToken          string    `json:"access_token"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
	RefreshToken         string    `json:"refresh_token"`
	TokenType            string    `json:"token_type"`
}

// Signer signs access tokens with the current Ed25519 key.
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
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, Claims{
		Issuer: s.issuer, Audience: jwt.ClaimStrings{s.audience}, Subject: x.accountID.String(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp),
		SessionID: x.id.String(),
		AuthTime:  x.authTime.Unix(),
	})
	t.Header["kid"] = s.kid
	signed, err := t.SignedString(s.key)
	return signed, exp, err
}

// Verifier checks access tokens against the current key and, during a key
// change, the previous one.
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

// Verify returns the principal of a valid token at now. Only EdDSA is
// accepted, which closes alg:none and algorithm-swap attacks. Every failure
// is ErrInvalidToken, carrying the cause for the log.
func (v Verifier) Verify(token string, now time.Time) (authn.Principal, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
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
		return authn.Principal{}, invalidToken(err)
	}
	account, err1 := uuid.Parse(c.Subject)
	sid, err2 := uuid.Parse(c.SessionID)
	if err1 != nil || err2 != nil || c.AuthTime == 0 {
		return authn.Principal{}, invalidToken(errors.New("sub, sid or auth_time missing"))
	}
	return authn.Principal{AccountID: account, SessionID: sid, AuthTime: time.Unix(c.AuthTime, 0).UTC()}, nil
}

func invalidToken(cause error) error {
	e := *ErrInvalidToken
	e.Err = cause
	return &e
}
