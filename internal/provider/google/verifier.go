// Package google checks the ID tokens Google issues to the app through
// Android Credential Manager. A token proves an email address.
package google

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/provider/jwks"
)

const (
	// freshFor is how long after issue a token signs in, as for phone tokens.
	freshFor = 5 * time.Minute
	skew     = 30 * time.Second
)

// ErrInvalidToken is a token that failed a check; the log says which.
var ErrInvalidToken = apperr.New(apperr.KindUnauthenticated, "invalid_google_token",
	"the Google token is not valid; sign in with Google again")

// Verifier checks the ID tokens of one OAuth client.
type Verifier struct {
	cfg  config.GoogleConfig
	keys *jwks.Set
	now  func() time.Time
	log  *slog.Logger
}

func NewVerifier(cfg config.GoogleConfig, now func() time.Time, log *slog.Logger) *Verifier {
	return &Verifier{cfg: cfg, keys: jwks.New(cfg.JWKSURL, now, log), now: now, log: log}
}

type claims struct {
	jwt.RegisteredClaims

	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// Verify returns the email address token proves: RS256 with a key of
// Google's, the client ID as audience, Google as issuer, issued less than 5
// minutes ago and the address verified. A failed check is ErrInvalidToken;
// keys that cannot be fetched are jwks.ErrUnavailable.
func (v *Verifier) Verify(ctx context.Context, token string) (contact.Address, error) {
	refuse := func(why string, err error) (contact.Address, error) {
		if errors.Is(err, jwks.ErrUnavailable) {
			return contact.Address{}, jwks.ErrUnavailable
		}
		v.log.InfoContext(ctx, "google token refused", "why", why, "err", err)
		return contact.Address{}, ErrInvalidToken
	}
	if v.cfg.ClientID == "" {
		return refuse("GOOGLE_CLIENT_ID is empty: Google sign-in is off", nil)
	}
	var c claims
	_, err := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience(v.cfg.ClientID),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(skew), jwt.WithTimeFunc(v.now),
	).ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string) //nolint:revive // a token without a kid finds no key
		return v.keys.Key(ctx, kid)
	})
	if err != nil {
		return refuse("signature or claims", err)
	}
	switch {
	case c.Issuer != "accounts.google.com" && c.Issuer != "https://accounts.google.com":
		return refuse("issuer", nil)
	case c.IssuedAt == nil || v.now().Sub(c.IssuedAt.Time) > freshFor:
		return refuse("not issued in the last 5 minutes", nil)
	case !c.EmailVerified:
		return refuse("email not verified", nil)
	}
	a, err := contact.ParseEmail(c.Email)
	if err != nil {
		return refuse("email", err)
	}
	return a, nil
}
