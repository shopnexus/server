package token

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server2/internal/platform/config"
)

var now = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

func keys(kid string) config.AuthConfig {
	k := config.NewAuthKeys()
	k.JWTKeyID, k.Issuer, k.Audience = kid, "server2-test", "unused"
	return k
}

func claims() Claims {
	return Claims{Subject: uuid.NewV7(), SessionID: uuid.NewV7(), AuthTime: now.Add(-time.Hour), Audience: "aud-a"}
}

func TestSignThenVerify(t *testing.T) {
	t.Parallel()
	k := keys("current")
	c := claims()
	tok, exp, err := NewSigner(k).Sign(c, 5*time.Minute, now)
	if err != nil || !exp.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("%v %v", exp, err)
	}
	got, err := NewVerifier(k).Verify(tok, "aud-a", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	c.ExpiresAt = exp
	if got != c {
		t.Fatalf("got %+v, want %+v", got, c)
	}
}

func TestOptionalClaimsStayZero(t *testing.T) {
	t.Parallel()
	k := keys("current")
	sub := uuid.NewV7()
	tok, _, _ := NewSigner(k).Sign(Claims{Subject: sub, Audience: "aud-a"}, time.Minute, now)
	got, err := NewVerifier(k).Verify(tok, "aud-a", now)
	if err != nil || got.Subject != sub || got.SessionID != (uuid.UUID{}) || !got.AuthTime.IsZero() {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestVerifyRejects(t *testing.T) {
	t.Parallel()
	k := keys("current")
	signer, verifier := NewSigner(k), NewVerifier(k)
	good, _, _ := signer.Sign(claims(), 5*time.Minute, now)
	raw := jwtClaims{
		Issuer: k.Issuer, Audience: jwt.ClaimStrings{"aud-a"}, Subject: uuid.NewV7().String(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		NotBefore: nil, ID: "",
		SessionID: uuid.NewV7().String(), AuthTime: now.Unix(),
	}
	none := jwt.NewWithClaims(jwt.SigningMethodNone, raw)
	none.Header["kid"] = k.JWTKeyID
	noneToken, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	swap := jwt.NewWithClaims(jwt.SigningMethodHS256, raw)
	swap.Header["kid"] = k.JWTKeyID
	swapToken, _ := swap.SignedString([]byte(k.JWTKey.Public().(ed25519.PublicKey)))
	forged, _, _ := NewSigner(keys("current")).Sign(claims(), 5*time.Minute, now)
	parts := strings.Split(good, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"` + uuid.NewV7().String() + `"}`))
	wrongIss := k
	wrongIss.Issuer = "someone-else"

	cases := []struct {
		name     string
		verifier Verifier
		token    string
		audience string
		at       time.Time
	}{
		{"expired past leeway", verifier, good, "aud-a", now.Add(5*time.Minute + 31*time.Second)},
		{"alg none", verifier, noneToken, "aud-a", now},
		{"HS256 keyed with the public key", verifier, swapToken, "aud-a", now},
		{"another key with the same kid", verifier, forged, "aud-a", now},
		{"tampered payload", verifier, strings.Join(parts, "."), "aud-a", now},
		{"another audience", verifier, good, "aud-b", now},
		{"another issuer", NewVerifier(wrongIss), good, "aud-a", now},
		{"garbage", verifier, "not.a.token", "aud-a", now},
		{"empty", verifier, "", "aud-a", now},
	}
	for _, c := range cases {
		if _, err := c.verifier.Verify(c.token, c.audience, c.at); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, err := verifier.Verify(good, "aud-a", now.Add(5*time.Minute+29*time.Second)); err != nil {
		t.Errorf("inside the leeway: %v", err)
	}
}

func TestVerifierKeyRotation(t *testing.T) {
	t.Parallel()
	old, current := keys("old"), keys("new")
	current.JWTPrevKeyID, current.JWTPrevKey = old.JWTKeyID, old.JWTKey.Public().(ed25519.PublicKey)
	oldToken, _, _ := NewSigner(old).Sign(claims(), time.Minute, now)
	if _, err := NewVerifier(current).Verify(oldToken, "aud-a", now); err != nil {
		t.Fatalf("token from the previous key: %v", err)
	}
	withoutPrev := current
	withoutPrev.JWTPrevKeyID, withoutPrev.JWTPrevKey = "", nil
	if _, err := NewVerifier(withoutPrev).Verify(oldToken, "aud-a", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("previous key dropped: %v", err)
	}
}

func TestNewOpaque(t *testing.T) {
	t.Parallel()
	tok, hash := NewOpaque("rt_")
	again, _ := NewOpaque("rt_")
	if !strings.HasPrefix(tok, "rt_") || len(tok) != 3+43 || !bytes.Equal(hash, Hash(tok)) || again == tok {
		t.Fatalf("%q", tok)
	}
}
