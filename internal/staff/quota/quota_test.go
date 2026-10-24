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

func TestFifthFailureLocks(t *testing.T) {
	t.Parallel()
	q := quota.New([]byte("k"))
	for i := 1; i < quota.MaxFailures; i++ {
		if q.Fail(now) {
			t.Fatalf("failure %d locked", i)
		}
	}
	if !q.Fail(now) {
		t.Fatal("the fifth failure did not lock")
	}
	if wait, locked := q.Locked(now.Add(time.Minute)); !locked || wait != quota.LockFor-time.Minute {
		t.Fatalf("locked %v for %v", locked, wait)
	}
	if _, locked := q.Locked(now.Add(quota.LockFor)); locked {
		t.Fatal("still locked after the lock ran out")
	}
	if q.Failures() != 0 {
		t.Fatal("the lock did not restart the count")
	}
}

func TestClear(t *testing.T) {
	t.Parallel()
	q := quota.New([]byte("k"))
	for range quota.MaxFailures {
		q.Fail(now)
	}
	q.Clear(now)
	if _, locked := q.Locked(now); locked || q.Failures() != 0 {
		t.Fatal("clear kept the lock")
	}
}

func TestKey(t *testing.T) {
	t.Parallel()
	mac := []byte("0123456789abcdef0123456789abcdef")
	a, b := quota.Key(mac, "mod@example.com"), quota.Key(mac, "mod@example.com")
	if !bytes.Equal(a, b) || len(a) != 32 || bytes.Contains(a, []byte("mod")) ||
		bytes.Equal(a, quota.Key([]byte("another key of thirty-two bytes!"), "mod@example.com")) {
		t.Fatal("key")
	}
}
