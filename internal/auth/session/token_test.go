package session_test

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/config"
)

func keys(kid string) config.AuthConfig {
	k := config.NewAuthKeys()
	k.JWTKeyID, k.Issuer, k.Audience = kid, "server2-test", "shopnexus-app-test"
	return k
}

func TestSignThenVerify(t *testing.T) {
	t.Parallel()
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

func TestVerifyRefusesAnotherAudience(t *testing.T) {
	t.Parallel()
	k := keys("current")
	good, _, _ := session.NewSigner(k).Sign(anySession(), now)
	other := k
	other.Audience = "shopnexus-staff"
	if _, err := session.NewVerifier(other).Verify(good, now); !errors.Is(err, session.ErrInvalidToken) {
		t.Fatalf("a customer token passed another audience: %v", err)
	}
}

func TestVerifierKeyRotation(t *testing.T) {
	t.Parallel()
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
