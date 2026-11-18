// Package googletest signs Google ID tokens with a key it serves from an
// httptest JWKS endpoint, for tests of Google sign-in.
package googletest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server/internal/platform/config"
)

const (
	clientID = "shopnexus-test.apps.googleusercontent.com"
	kid      = "google-1"
)

// Issuer holds the RSA key Google signs ID tokens with and serves it.
type Issuer struct {
	// Now is the time tokens are issued at; time.Now unless a test sets it.
	Now  func() time.Time
	key  *rsa.PrivateKey
	srv  *httptest.Server
	down atomic.Bool
}

// New starts the key server; t's cleanup stops it.
func New(tb testing.TB) *Issuer {
	tb.Helper()
	i, stop, err := NewForMain()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(stop)
	return i
}

// NewForMain is New for TestMain, which has no t; call stop when done.
func NewForMain() (i *Issuer, stop func(), err error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("rsa key: %w", err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	body, _ := json.Marshal(map[string]any{"keys": []any{map[string]string{ //nolint:errchkjson // a map of strings marshals
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid, "n": enc(k.N.Bytes()), "e": enc(big.NewInt(int64(k.E)).Bytes()),
	}}})
	i = &Issuer{Now: time.Now, key: k}
	i.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if i.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	}))
	return i, i.srv.Close, nil
}

// Config points a server at this key.
func (i *Issuer) Config() config.GoogleConfig {
	return config.GoogleConfig{ClientID: clientID, JWKSURL: i.srv.URL}
}

// SetDown makes the key URL answer 503.
func (i *Issuer) SetDown(down bool) { i.down.Store(down) }

// token is what Token signs; each Option changes it first.
type token struct {
	method jwt.SigningMethod
	key    any
	claims jwt.MapClaims
}

type Option func(*token)

func Claim(name string, value any) Option { return func(t *token) { t.claims[name] = value } }
func Drop(name string) Option             { return func(t *token) { delete(t.claims, name) } }

// SignedWith signs with another method and key; the kid stays.
func SignedWith(method jwt.SigningMethod, key any) Option {
	return func(t *token) { t.method, t.key = method, key }
}

// Token is a Google ID token for a verified email, issued now.
func (i *Issuer) Token(email string, opts ...Option) string {
	now := i.Now()
	t := token{method: jwt.SigningMethodRS256, key: i.key, claims: jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": clientID, "azp": "android-client", "sub": strconv.FormatInt(now.UnixNano(), 10),
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "email": email, "email_verified": true,
	}}
	for _, o := range opts {
		o(&t)
	}
	tok := jwt.NewWithClaims(t.method, t.claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(t.key)
	if err != nil {
		panic(fmt.Sprintf("googletest: sign: %v", err)) // an option built a token no key signs: a test bug
	}
	return s
}
