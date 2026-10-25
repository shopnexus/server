package domain

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/token"
)

// Tokens is what a sign-in or a refresh returns.
type Tokens struct {
	AccessToken          string    `json:"access_token"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
	RefreshToken         string    `json:"refresh_token"`
	TokenType            string    `json:"token_type"`
}

// Signer signs customer access tokens.
type Signer struct {
	t        token.Signer
	audience string
}

func NewSigner(cfg config.AuthConfig) Signer {
	return Signer{t: token.NewSigner(cfg), audience: cfg.Audience}
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
	signed, exp, err := s.t.Sign(token.Claims{
		Subject: x.accountID, SessionID: x.id, AuthTime: x.authTime, Audience: s.audience,
	}, AccessTTL, now)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, exp, nil
}

// Verifier checks customer access tokens.
type Verifier struct {
	t        token.Verifier
	audience string
}

func NewVerifier(cfg config.AuthConfig) Verifier {
	return Verifier{t: token.NewVerifier(cfg), audience: cfg.Audience}
}

// Verify returns the principal of a valid token at now. A customer token
// carries a session and an auth time; one without either is invalid.
func (v Verifier) Verify(tok string, now time.Time) (authn.Principal, error) {
	c, err := v.t.Verify(tok, v.audience, now)
	if err != nil {
		return authn.Principal{}, fmt.Errorf("verify access token: %w", err)
	}
	if c.SessionID == (uuid.UUID{}) || c.AuthTime.IsZero() {
		e := *ErrInvalidToken
		e.Err = errors.New("sid or auth_time missing")
		return authn.Principal{}, &e
	}
	return authn.Principal{AccountID: c.Subject, SessionID: c.SessionID, AuthTime: c.AuthTime}, nil
}
