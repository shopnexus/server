package quota_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/staff/quota"
)

func failed() audit.Entry {
	return audit.Entry{Actor: audit.Anonymous(clientIP), Action: "staff.login_failed", Target: audit.Target{Kind: "", ID: uuid.UUID{}}, Diff: nil, At: now}
}

func newKey() []byte {
	return quota.Key([]byte("0123456789abcdef0123456789abcdef"), uuid.NewV7().String())
}

func TestFifthFailureLocks(t *testing.T) {
	t.Parallel()
	s := quota.NewPGStore(pgtest.New(t))
	ctx := context.Background()
	key := newKey()
	for i := 1; i <= quota.MaxFailures; i++ {
		adm, err := s.Charge(ctx, key, now)
		if err != nil || !adm.Admitted || adm.Charged != i {
			t.Fatalf("attempt %d: %+v %v", i, adm, err)
		}
		if err := s.Fail(ctx, key, adm.Charged, now, failed()); err != nil {
			t.Fatal(err)
		}
	}
	adm, err := s.Charge(ctx, key, now.Add(time.Minute))
	if err != nil || adm.Admitted || adm.RetryAfter != quota.LockFor-time.Minute {
		t.Fatalf("while locked: %+v %v", adm, err)
	}
	locked, _ := s.LockedUntil(ctx, [][]byte{key, []byte("other")}, now)
	if !locked[string(key)].Equal(now.Add(quota.LockFor)) || len(locked) != 1 {
		t.Fatalf("locks: %v", locked)
	}
	if adm, _ = s.Charge(ctx, key, now.Add(quota.LockFor)); !adm.Admitted || adm.Charged != 1 {
		t.Fatalf("after the lock: %+v", adm)
	}
}

// Five attempts in flight take the whole budget, so a sixth is refused
// before anything is checked.
func TestChargeRefusesPastTheBudget(t *testing.T) {
	t.Parallel()
	s := quota.NewPGStore(pgtest.New(t))
	ctx := context.Background()
	key := newKey()
	for range quota.MaxFailures {
		if adm, err := s.Charge(ctx, key, now); err != nil || !adm.Admitted {
			t.Fatalf("%+v %v", adm, err)
		}
	}
	if adm, err := s.Charge(ctx, key, now); err != nil || adm.Admitted || adm.RetryAfter != time.Second {
		t.Fatalf("a sixth attempt in flight: %+v %v", adm, err)
	}
}

func TestRefundAndClear(t *testing.T) {
	t.Parallel()
	s := quota.NewPGStore(pgtest.New(t))
	ctx := context.Background()
	key := newKey()
	_, _ = s.Charge(ctx, key, now)
	if err := s.Refund(ctx, key, now); err != nil {
		t.Fatal(err)
	}
	if q, _ := s.Get(ctx, key); q.Failures() != 0 {
		t.Fatalf("after a refund: %d", q.Failures())
	}
	for range quota.MaxFailures {
		adm, _ := s.Charge(ctx, key, now)
		_ = s.Fail(ctx, key, adm.Charged, now, failed())
	}
	if err := s.Clear(ctx, key, now); err != nil {
		t.Fatal(err)
	}
	if adm, _ := s.Charge(ctx, key, now); !adm.Admitted {
		t.Fatal("still locked after Clear")
	}
}

func TestSaveAuditedConflicts(t *testing.T) {
	t.Parallel()
	s := quota.NewPGStore(pgtest.New(t))
	ctx := context.Background()
	key := newKey()
	_, _ = s.Charge(ctx, key, now)
	q, _ := s.Get(ctx, key)
	stale := q
	q.Clear(now)
	if err := s.SaveAudited(ctx, &q, uuid.NewV7(), failed()); err != nil {
		t.Fatal(err)
	}
	stale.Clear(now)
	if err := s.Save(ctx, &stale, uuid.NewV7()); err == nil {
		t.Fatal("a stale save went through")
	}
}
