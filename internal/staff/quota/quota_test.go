package quota_test

import (
	"bytes"
	"net/netip"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/staff/quota"
)

var (
	now      = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	clientIP = netip.MustParseAddr("203.0.113.7")
)

func TestKey(t *testing.T) {
	t.Parallel()
	mac := []byte("0123456789abcdef0123456789abcdef")
	a, b := quota.Key(mac, "mod@example.com"), quota.Key(mac, "mod@example.com")
	if !bytes.Equal(a, b) || len(a) != 32 || bytes.Contains(a, []byte("mod")) ||
		bytes.Equal(a, quota.Key([]byte("another key of thirty-two bytes!"), "mod@example.com")) {
		t.Fatal("key")
	}
}

func TestNewIsUnlocked(t *testing.T) {
	t.Parallel()
	q := quota.New([]byte("k"))
	if _, locked := q.Locked(now); locked || q.Failures() != 0 {
		t.Fatal("a new quota is locked")
	}
}
