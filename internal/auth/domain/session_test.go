package domain_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
)

func TestNewSession(t *testing.T) {
	t.Parallel()
	id, acc := uuid.NewV7(), uuid.NewV7()
	s, token := domain.NewSession(id, acc, "Pixel 9", now.Add(-time.Minute), now)
	if !strings.HasPrefix(token, "rt_") || len(token) != 3+43 || !bytes.Equal(s.RefreshHash(), domain.HashRefresh(token)) {
		t.Fatalf("token %q", token)
	}
	if s.ID() != id || s.WriteID() != id || s.AccountID() != acc || s.DeviceName() != "Pixel 9" ||
		!s.AuthTime().Equal(now.Add(-time.Minute)) || !s.ExpiresAt().Equal(now.Add(domain.SessionMaxAge)) || s.PrevRefreshHash() != nil {
		t.Fatalf("%+v", s)
	}
}

func TestRotateKeepsThePreviousHash(t *testing.T) {
	t.Parallel()
	s, first := domain.NewSession(uuid.NewV7(), uuid.NewV7(), "", now, now)
	second := s.Rotate(now.Add(time.Hour))
	if second == first || !bytes.Equal(s.RefreshHash(), domain.HashRefresh(second)) ||
		!bytes.Equal(s.PrevRefreshHash(), domain.HashRefresh(first)) || !s.LastUsedAt().Equal(now.Add(time.Hour)) {
		t.Fatalf("%+v", s)
	}
}

func TestUsable(t *testing.T) {
	t.Parallel()
	s, _ := domain.NewSession(uuid.NewV7(), uuid.NewV7(), "", now, now)
	if err := s.Usable(now); err != nil {
		t.Fatal(err)
	}
	if err := s.Usable(now.Add(domain.SessionIdleLimit)); !errors.Is(err, domain.ErrExpired) {
		t.Fatalf("idle: %v", err)
	}
	s.Rotate(now.Add(50 * 24 * time.Hour))
	if err := s.Usable(now.Add(59 * 24 * time.Hour)); err != nil {
		t.Fatalf("used recently, old: %v", err)
	}
	if err := s.Usable(now.Add(domain.SessionMaxAge)); !errors.Is(err, domain.ErrExpired) {
		t.Fatalf("max age: %v", err)
	}
	s.Revoke(now)
	if err := s.Usable(now); !errors.Is(err, domain.ErrRevoked) {
		t.Fatalf("revoked: %v", err)
	}
}
