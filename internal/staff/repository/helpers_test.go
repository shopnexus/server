package repository_test

import (
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/staff/domain"
)

var (
	now      = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	clientIP = netip.MustParseAddr("203.0.113.7")
)

func newMember(t *testing.T, role domain.Role) domain.Member {
	t.Helper()
	m, err := domain.NewMember(uuid.NewV7(), " Mod@Example.COM ", " Mod One ", role, domain.Fast.Hash("temporary-pass"), now)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
