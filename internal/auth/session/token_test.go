package session_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/config"
)

func keys(kid string) config.AuthConfig {
	k := config.NewAuthKeys()
	k.JWTKeyID = kid
	return k
}

func TestSignThenVerify(t *testing.T) {
	k := keys("current")
	acc, sid, authTime := uuid.NewV7(), uuid.NewV7(), now.Add(-time.Hour)
	s, _ := session.New(sid, acc, "", authTime, now)
	token, exp, err := session.NewSigner(k).Sign(s, now)
	if err != nil || !exp.Equal(now.Add(session.AccessTTL)) {
		t.Fatalf("%v %v", exp, err)
	}
	p, err := session.NewVerifier(k).Verify(token, now.Add(time.Minute))
	if err != nil || p.AccountID != acc || p.SessionID != sid || !p.AuthTime.Equal(authTime) {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestVerifyRejects(t *testing.T) {
	k := keys("current")
	signer, verifier := session.NewSigner(k), session.NewVerifier(k)
	good, _, _ := signer.Sign(anySession(), now)
	claims := session.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: k.Issuer, Audience: jwt.ClaimStrings{k.Audience}, Subject: uuid.NewV7().String(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(session.AccessTTL)),
		},
		SessionID: uuid.NewV7().String(), AuthTime: now.Unix(),
	}
	none := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	none.Header["kid"] = k.JWTKeyID
	noneToken, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	swap := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	swap.Header["kid"] = k.JWTKeyID
	swapToken, _ := swap.SignedString([]byte(k.JWTKey.Public().(ed25519.PublicKey)))
	forged, _, _ := session.NewSigner(keys("current")).Sign(anySession(), now)
	parts := strings.Split(good, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"` + uuid.NewV7().String() + `"}`))
	wrongAud, wrongIss := k, k
	wrongAud.Audience, wrongIss.Issuer = "another-app", "someone-else"

	cases := []struct {
		name     string
		verifier session.Verifier
		token    string
		at       time.Time
	}{
		{"expired past leeway", verifier, good, now.Add(session.AccessTTL + 31*time.Second)},
		{"alg none", verifier, noneToken, now},
		{"HS256 keyed with the public key", verifier, swapToken, now},
		{"another key with the same kid", verifier, forged, now},
		{"tampered payload", verifier, strings.Join(parts, "."), now},
		{"another audience", session.NewVerifier(wrongAud), good, now},
		{"another issuer", session.NewVerifier(wrongIss), good, now},
		{"garbage", verifier, "not.a.token", now},
		{"empty", verifier, "", now},
	}
	for _, c := range cases {
		if _, err := c.verifier.Verify(c.token, c.at); !errors.Is(err, session.ErrInvalidToken) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, err := verifier.Verify(good, now.Add(session.AccessTTL+29*time.Second)); err != nil {
		t.Errorf("inside the leeway: %v", err)
	}
}

func TestVerifierKeyRotation(t *testing.T) {
	old, current := keys("old"), keys("new")
	current.JWTPrevKeyID, current.JWTPrevKey = old.JWTKeyID, old.JWTKey.Public().(ed25519.PublicKey)
	oldToken, _, _ := session.NewSigner(old).Sign(anySession(), now)
	if _, err := session.NewVerifier(current).Verify(oldToken, now); err != nil {
		t.Fatalf("token from the previous key: %v", err)
	}
	withoutPrev := current
	withoutPrev.JWTPrevKeyID, withoutPrev.JWTPrevKey = "", nil
	if _, err := session.NewVerifier(withoutPrev).Verify(oldToken, now); !errors.Is(err, session.ErrInvalidToken) {
		t.Fatalf("previous key dropped: %v", err)
	}
	ghost, _, _ := session.NewSigner(keys("ghost")).Sign(anySession(), now)
	if _, err := session.NewVerifier(current).Verify(ghost, now); !errors.Is(err, session.ErrInvalidToken) {
		t.Fatalf("unknown kid: %v", err)
	}
}

func anySession() session.Session {
	s, _ := session.New(uuid.NewV7(), uuid.NewV7(), "", now, now)
	return s
}
