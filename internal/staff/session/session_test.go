package session_test

import (
	"bytes"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/session"
)

var (
	now      = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	clientIP = netip.MustParseAddr("203.0.113.7")
)

func TestNewAndRotate(t *testing.T) {
	t.Parallel()
	s, refresh := session.New(uuid.NewV7(), uuid.NewV7(), 4, clientIP, strings.Repeat("a", 600), now)
	if !s.ExpiresAt().Equal(now.Add(session.TTL)) || s.CredentialsVersion() != 4 || len([]rune(s.UserAgent())) != 500 {
		t.Fatalf("%+v", s)
	}
	next := s.Rotate(now.Add(time.Minute))
	if next == refresh || !bytes.Equal(s.PrevRefreshHash(), token.HashRefresh(refresh)) {
		t.Fatal("rotate")
	}
}

func TestUsable(t *testing.T) {
	t.Parallel()
	s, _ := session.New(uuid.NewV7(), uuid.NewV7(), 1, clientIP, "", now)
	if s.Usable(now.Add(session.TTL-time.Second)) != nil {
		t.Fatal("usable inside the eight hours")
	}
	if err := s.Usable(now.Add(session.TTL)); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("at eight hours: %v", err)
	}
	s.Revoke(now)
	if err := s.Usable(now); !errors.Is(err, session.ErrRevoked) {
		t.Fatalf("revoked: %v", err)
	}
}

func TestGrantCheck(t *testing.T) {
	t.Parallel()
	ok := session.Grant{Role: member.RoleModerator, Active: true, Current: true, RevokedAt: nil, ExpiresAt: now.Add(time.Hour)}
	if ok.Check(now) != nil {
		t.Fatal("a live grant")
	}
	revoked, inactive, old, expired := ok, ok, ok, ok
	revoked.RevokedAt = &now
	inactive.Active = false
	old.Current = false
	expired.ExpiresAt = now
	for name, g := range map[string]session.Grant{"revoked": revoked, "disabled member": inactive, "old version": old} {
		if err := g.Check(now); !errors.Is(err, session.ErrRevoked) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := expired.Check(now); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("expired: %v", err)
	}
}
