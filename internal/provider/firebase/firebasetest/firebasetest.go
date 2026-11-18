// Package firebasetest signs phone tokens the way Firebase does, with keys
// it serves from httptest JWKS endpoints, for tests of the sign-in path.
package firebasetest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server/internal/platform/config"
)

const (
	projectID     = "shopnexus-test"
	projectNumber = "123456789"
	rsaKid        = "rsa-1"
	ecKid         = "ec-1"
)

// Issuer holds an RSA key for Firebase Authentication tokens and an EC key
// for PNV tokens, and serves each from its own JWKS URL.
type Issuer struct {
	// Now is the time tokens are issued at; time.Now unless a test sets it.
	Now     func() time.Time
	rsa     *rsa.PrivateKey
	ec      *ecdsa.PrivateKey
	auth    *httptest.Server
	pnv     *httptest.Server
	pnvDown atomic.Bool
}

// New starts the key servers; t's cleanup stops them.
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
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("rsa key: %w", err)
	}
	ek, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("ec key: %w", err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	i = &Issuer{Now: time.Now, rsa: rk, ec: ek}
	i.auth = httptest.NewServer(serve(func() bool { return false }, map[string]string{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": rsaKid,
		"n": enc(rk.N.Bytes()), "e": enc(big.NewInt(int64(rk.E)).Bytes()),
	}))
	pub, err := ek.PublicKey.Bytes() // 0x04 || X || Y
	if err != nil {
		i.auth.Close()
		return nil, nil, fmt.Errorf("ec point: %w", err)
	}
	i.pnv = httptest.NewServer(serve(i.pnvDown.Load, map[string]string{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": ecKid, "x": enc(pub[1:33]), "y": enc(pub[33:]),
	}))
	return i, func() { i.auth.Close(); i.pnv.Close() }, nil
}

func serve(down func() bool, key map[string]string) http.Handler {
	body, _ := json.Marshal(map[string]any{"keys": []any{key}}) //nolint:errchkjson // a map of strings marshals
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	})
}

// Config points a server at these keys.
func (i *Issuer) Config(testNumbers bool) config.FirebaseConfig {
	return config.FirebaseConfig{
		ProjectID: projectID, ProjectNumber: projectNumber, AuthJWKSURL: i.auth.URL, PNVJWKSURL: i.pnv.URL,
		TestNumbers: testNumbers,
	}
}

// SetPNVDown makes the PNV key URL answer 503.
func (i *Issuer) SetPNVDown(down bool) { i.pnvDown.Store(down) }

// token is what SMSToken and PNVToken sign; each Option changes it first.
type token struct {
	method jwt.SigningMethod
	key    any
	header map[string]any
	claims jwt.MapClaims
}

type Option func(*token)

func Claim(name string, value any) Option { return func(t *token) { t.claims[name] = value } }
func Drop(name string) Option             { return func(t *token) { delete(t.claims, name) } }
func Header(name string, value any) Option {
	return func(t *token) { t.header[name] = value }
}

// SignedWith signs with another method and key; the kid stays.
func SignedWith(method jwt.SigningMethod, key any) Option {
	return func(t *token) { t.method, t.key = method, key }
}

// SMSToken is a Firebase Authentication ID token for phone, verified now.
func (i *Issuer) SMSToken(phone string, opts ...Option) string {
	now := i.Now()
	return sign(token{
		method: jwt.SigningMethodRS256, key: i.rsa, header: map[string]any{"kid": rsaKid},
		claims: jwt.MapClaims{
			"iss": "https://securetoken.google.com/" + projectID, "aud": projectID, "sub": "uid-" + phone,
			"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "auth_time": now.Unix(), "phone_number": phone,
			"firebase": map[string]any{"sign_in_provider": "phone", "identities": map[string]any{"phone": []string{phone}}},
		},
	}, opts)
}

// PNVToken is a Phone Number Verification token for phone, issued now.
func (i *Issuer) PNVToken(phone string, opts ...Option) string {
	now := i.Now()
	return sign(token{
		method: jwt.SigningMethodES256, key: i.ec, header: map[string]any{"kid": ecKid, "typ": "JWT"},
		claims: jwt.MapClaims{
			"iss": "https://fpnv.googleapis.com/projects/" + projectNumber,
			"aud": []string{
				"https://fpnv.googleapis.com/projects/" + projectNumber, "https://fpnv.googleapis.com/projects/" + projectID,
			},
			"sub": phone, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		},
	}, opts)
}

func sign(t token, opts []Option) string {
	for _, o := range opts {
		o(&t)
	}
	tok := jwt.NewWithClaims(t.method, t.claims)
	maps.Copy(tok.Header, t.header)
	s, err := tok.SignedString(t.key)
	if err != nil {
		panic(fmt.Sprintf("firebasetest: sign: %v", err)) // an option built a token no key signs: a test bug
	}
	return s
}
