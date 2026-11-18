package domain_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/config"
)

func keys(kid string) config.AuthConfig {
	k := config.NewAuthKeys()
	k.JWTKeyID, k.Issuer, k.Audience = kid, "server-test", "shopnexus-app-test"
	return k
}

func TestSignThenVerify(t *testing.T) {
	t.Parallel()
	k := keys("current")
	acc, sid, authTime := uuid.NewV7(), uuid.NewV7(), now.Add(-time.Hour)
	s, _ := domain.NewSession(sid, acc, "", authTime, now)
	token, exp, err := domain.NewSigner(k).Sign(s, now)
	if err != nil || !exp.Equal(now.Add(domain.AccessTTL)) {
		t.Fatalf("%v %v", exp, err)
	}
	p, err := domain.NewVerifier(k).Verify(token, now.Add(time.Minute))
	if err != nil || p.AccountID != acc || p.SessionID != sid || !p.AuthTime.Equal(authTime) || !p.ExpiresAt.Equal(exp) {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestVerifyRefusesAnotherAudience(t *testing.T) {
	t.Parallel()
	k := keys("current")
	good, _, _ := domain.NewSigner(k).Sign(anySession(), now)
	other := k
	other.Audience = "shopnexus-console"
	if _, err := domain.NewVerifier(other).Verify(good, now); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("a customer token passed another audience: %v", err)
	}
}

func TestVerifierKeyRotation(t *testing.T) {
	t.Parallel()
	old, current := keys("old"), keys("new")
	current.JWTPrevKeyID, current.JWTPrevKey = old.JWTKeyID, old.JWTKey.Public().(ed25519.PublicKey)
	oldToken, _, _ := domain.NewSigner(old).Sign(anySession(), now)
	if _, err := domain.NewVerifier(current).Verify(oldToken, now); err != nil {
		t.Fatalf("token from the previous key: %v", err)
	}
	withoutPrev := current
	withoutPrev.JWTPrevKeyID, withoutPrev.JWTPrevKey = "", nil
	if _, err := domain.NewVerifier(withoutPrev).Verify(oldToken, now); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("previous key dropped: %v", err)
	}
	ghost, _, _ := domain.NewSigner(keys("ghost")).Sign(anySession(), now)
	if _, err := domain.NewVerifier(current).Verify(ghost, now); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("unknown kid: %v", err)
	}
}

// raw signs claims with k's key as the current kid, outside Signer.
func raw(t *testing.T, k config.AuthConfig, m jwt.SigningMethod, key any, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(m, claims)
	tok.Header["kid"] = k.JWTKeyID
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestVerifyRejects(t *testing.T) {
	t.Parallel()
	k := keys("current")
	signer, verifier := domain.NewSigner(k), domain.NewVerifier(k)
	good, _, err := signer.Sign(anySession(), now)
	if err != nil {
		t.Fatal(err)
	}
	claims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss": k.Issuer, "aud": k.Audience, "sub": uuid.NewV7().String(), "iat": now.Unix(),
			"exp": now.Add(5 * time.Minute).Unix(), "sid": uuid.NewV7().String(), "auth_time": now.Unix(),
		}
	}
	without := func(name string) jwt.MapClaims {
		c := claims()
		delete(c, name)
		return c
	}
	noneToken := raw(t, k, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claims())
	swapToken := raw(t, k, jwt.SigningMethodHS256, []byte(k.JWTKey.Public().(ed25519.PublicKey)), claims())
	forged, _, err := domain.NewSigner(keys("current")).Sign(anySession(), now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(good, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"` + uuid.NewV7().String() + `"}`))
	wrongIss := k
	wrongIss.Issuer = "someone-else"

	cases := []struct {
		name     string
		verifier domain.Verifier
		token    string
		at       time.Time
	}{
		{"expired past leeway", verifier, good, now.Add(domain.AccessTTL + 31*time.Second)},
		{"alg none", verifier, noneToken, now},
		{"HS256 keyed with the public key", verifier, swapToken, now},
		{"another key with the same kid", verifier, forged, now},
		{"tampered payload", verifier, strings.Join(parts, "."), now},
		{"another issuer", domain.NewVerifier(wrongIss), good, now},
		{"no sid", verifier, raw(t, k, jwt.SigningMethodEdDSA, k.JWTKey, without("sid")), now},
		{"no auth_time", verifier, raw(t, k, jwt.SigningMethodEdDSA, k.JWTKey, without("auth_time")), now},
		{"no exp", verifier, raw(t, k, jwt.SigningMethodEdDSA, k.JWTKey, without("exp")), now},
		{"garbage", verifier, "not.a.token", now},
		{"empty", verifier, "", now},
	}
	for _, c := range cases {
		if _, err := c.verifier.Verify(c.token, c.at); !errors.Is(err, domain.ErrInvalidToken) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, err := verifier.Verify(good, now.Add(domain.AccessTTL+29*time.Second)); err != nil {
		t.Errorf("inside the leeway: %v", err)
	}
	if _, err := verifier.Verify(raw(t, k, jwt.SigningMethodEdDSA, k.JWTKey, claims()), now); err != nil {
		t.Errorf("the hand-built token itself: %v", err)
	}
}

func anySession() domain.Session {
	s, _ := domain.NewSession(uuid.NewV7(), uuid.NewV7(), "", now, now)
	return s
}
