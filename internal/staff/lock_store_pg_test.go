package staff_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/staff"
)

func newKey() []byte { return []byte(uuid.NewV7().String()) }

// retryAfter is the wait a refused Charge names, or -1 when it admitted.
func retryAfter(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return -1
	}
	e, ok := errors.AsType[*apperr.Error](err)
	if !ok || !errors.Is(err, staff.ErrLoginLocked) {
		t.Fatalf("not a lock: %v", err)
	}
	wait, ok := e.Details["retry_after"].(int)
	if !ok {
		t.Fatalf("retry_after: %v", e.Details)
	}
	return wait
}

func TestFifthFailureLocks(t *testing.T) {
	t.Parallel()
	s := staff.NewPGLockStore(pgtest.New(t))
	ctx := context.Background()
	key := newKey()
	for i := 1; i <= staff.MaxFailures; i++ {
		if err := s.Charge(ctx, key, now); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if err := s.Fail(ctx, key, now); err != nil {
			t.Fatal(err)
		}
	}
	if wait := retryAfter(t, s.Charge(ctx, key, now.Add(time.Minute))); wait != int((staff.LockFor - time.Minute).Seconds()) {
		t.Fatalf("while locked: wait %d", wait)
	}
	locked, _ := s.LockedUntil(ctx, [][]byte{key, []byte("other")}, now)
	if !locked[string(key)].Equal(now.Add(staff.LockFor)) || len(locked) != 1 {
		t.Fatalf("locks: %v", locked)
	}
	if err := s.Charge(ctx, key, now.Add(staff.LockFor)); err != nil {
		t.Fatalf("after the lock: %v", err)
	}
}

// Five attempts in flight take the whole budget, so a sixth is refused
// before anything is checked.
func TestChargeRefusesPastTheBudget(t *testing.T) {
	t.Parallel()
	s := staff.NewPGLockStore(pgtest.New(t))
	ctx := context.Background()
	key := newKey()
	for range staff.MaxFailures {
		if err := s.Charge(ctx, key, now); err != nil {
			t.Fatal(err)
		}
	}
	if wait := retryAfter(t, s.Charge(ctx, key, now)); wait != 1 {
		t.Fatalf("a sixth attempt in flight: wait %d", wait)
	}
}

func TestRefundAndUnlock(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := staff.NewPGLockStore(db)
	ctx := context.Background()
	key := newKey()
	for range 2 * staff.MaxFailures {
		if err := s.Charge(ctx, key, now); err != nil {
			t.Fatal(err)
		}
		if err := s.Refund(ctx, key, now); err != nil {
			t.Fatal(err)
		}
	}
	for range staff.MaxFailures {
		_ = s.Charge(ctx, key, now)
		_ = s.Fail(ctx, key, now)
	}
	target := uuid.NewV7()
	if err := s.Unlock(ctx, key, now, audit.Entry{Actor: audit.ByCLI("root"), Action: "staff.unlock", Target: audit.Staff(target), At: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.Charge(ctx, key, now); err != nil {
		t.Fatalf("still locked after Unlock: %v", err)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM staff_actions WHERE action = 'staff.unlock'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit rows %d: %v", n, err)
	}
}
