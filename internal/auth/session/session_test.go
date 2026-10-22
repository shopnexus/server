package session_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/session"
)

var now = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

func TestNewSession(t *testing.T) {
	t.Parallel()
	id, acc := uuid.NewV7(), uuid.NewV7()
	s, token := session.New(id, acc, "Pixel 9", now.Add(-time.Minute), now)
	if !strings.HasPrefix(token, "rt_") || len(token) != 3+43 || !bytes.Equal(s.RefreshHash(), session.HashRefresh(token)) {
		t.Fatalf("token %q", token)
	}
	if s.ID() != id || s.WriteID() != id || s.AccountID() != acc || s.DeviceName() != "Pixel 9" ||
		!s.AuthTime().Equal(now.Add(-time.Minute)) || !s.ExpiresAt().Equal(now.Add(session.MaxAge)) || s.PrevRefreshHash() != nil {
		t.Fatalf("%+v", s)
	}
}

func TestRotateKeepsThePreviousHash(t *testing.T) {
	t.Parallel()
	s, first := session.New(uuid.NewV7(), uuid.NewV7(), "", now, now)
	second := s.Rotate(now.Add(time.Hour))
	if second == first || !bytes.Equal(s.RefreshHash(), session.HashRefresh(second)) ||
		!bytes.Equal(s.PrevRefreshHash(), session.HashRefresh(first)) || !s.LastUsedAt().Equal(now.Add(time.Hour)) {
		t.Fatalf("%+v", s)
	}
}

func TestUsable(t *testing.T) {
	t.Parallel()
	s, _ := session.New(uuid.NewV7(), uuid.NewV7(), "", now, now)
	if err := s.Usable(now); err != nil {
		t.Fatal(err)
	}
	if err := s.Usable(now.Add(session.IdleLimit)); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("idle: %v", err)
	}
	s.Rotate(now.Add(50 * 24 * time.Hour))
	if err := s.Usable(now.Add(59 * 24 * time.Hour)); err != nil {
		t.Fatalf("used recently, old: %v", err)
	}
	if err := s.Usable(now.Add(session.MaxAge)); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("max age: %v", err)
	}
	s.Revoke(now)
	if err := s.Usable(now); !errors.Is(err, session.ErrRevoked) {
		t.Fatalf("revoked: %v", err)
	}
}
