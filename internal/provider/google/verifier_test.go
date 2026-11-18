package google_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server/internal/provider/google"
	"github.com/shopnexus/server/internal/provider/google/googletest"
	"github.com/shopnexus/server/internal/provider/jwks"
)

var at = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

func issuer(t *testing.T) *googletest.Issuer {
	t.Helper()
	i := googletest.New(t)
	i.Now = func() time.Time { return at }
	return i
}

func verifier(i *googletest.Issuer) *google.Verifier {
	return google.NewVerifier(i.Config(), func() time.Time { return at }, slog.New(slog.DiscardHandler))
}

func TestVerifyGoogleToken(t *testing.T) {
	t.Parallel()
	i := issuer(t)
	for _, iss := range []string{"https://accounts.google.com", "accounts.google.com"} {
		a, err := verifier(i).Verify(context.Background(), i.Token("Linh@Gmail.com", googletest.Claim("iss", iss)))
		if err != nil || a.Value() != "linh@gmail.com" {
			t.Errorf("iss %s: %v %v", iss, a, err)
		}
	}
}

func TestVerifyGoogleTokenRefuses(t *testing.T) {
	t.Parallel()
	i := issuer(t)
	stranger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for name, opt := range map[string]googletest.Option{
		"another key":        googletest.SignedWith(jwt.SigningMethodRS256, stranger),
		"hmac":               googletest.SignedWith(jwt.SigningMethodHS256, []byte("secret")),
		"another issuer":     googletest.Claim("iss", "https://evil.example"),
		"another client":     googletest.Claim("aud", "other.apps.googleusercontent.com"),
		"expired":            googletest.Claim("exp", at.Add(-time.Minute).Unix()),
		"no expiry":          googletest.Drop("exp"),
		"issued 6 min ago":   googletest.Claim("iat", at.Add(-6*time.Minute).Unix()),
		"issued in a minute": googletest.Claim("iat", at.Add(time.Minute).Unix()),
		"email not verified": googletest.Claim("email_verified", false),
		"no email":           googletest.Drop("email"),
		"no address":         googletest.Claim("email", "linh"),
	} {
		if _, err := verifier(i).Verify(context.Background(), i.Token("linh@gmail.com", opt)); !errors.Is(err, google.ErrInvalidToken) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := verifier(i).Verify(context.Background(), "not.a.token"); !errors.Is(err, google.ErrInvalidToken) {
		t.Errorf("malformed: %v", err)
	}
}

// Without GOOGLE_CLIENT_ID every token is refused.
func TestVerifyGoogleTokenWhenOff(t *testing.T) {
	t.Parallel()
	i := issuer(t)
	cfg := i.Config()
	cfg.ClientID = ""
	v := google.NewVerifier(cfg, func() time.Time { return at }, slog.New(slog.DiscardHandler))
	if _, err := v.Verify(context.Background(), i.Token("linh@gmail.com")); !errors.Is(err, google.ErrInvalidToken) {
		t.Fatalf("%v", err)
	}
}

func TestVerifyGoogleTokenWithoutKeys(t *testing.T) {
	t.Parallel()
	i := issuer(t)
	i.SetDown(true)
	if _, err := verifier(i).Verify(context.Background(), i.Token("linh@gmail.com")); !errors.Is(err, jwks.ErrUnavailable) {
		t.Fatalf("%v", err)
	}
}
