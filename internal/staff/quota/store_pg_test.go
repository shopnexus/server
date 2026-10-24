package quota_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/staff/quota"
)

func TestStore(t *testing.T) {
	t.Parallel()
	s := quota.NewPGStore(pgtest.New(t))
	ctx := context.Background()
	key := quota.Key([]byte("0123456789abcdef0123456789abcdef"), uuid.NewV7().String())

	q, err := s.Get(ctx, key)
	if err != nil || q.Failures() != 0 {
		t.Fatalf("a missing row: %+v %v", q, err)
	}
	racing := q
	q.Fail(now)
	if err = s.SaveAudited(ctx, &q, uuid.NewV7(), audit.Entry{Actor: audit.Anonymous(clientIP), Action: "staff.login_failed", Target: audit.Target{Kind: "", ID: uuid.UUID{}}, Diff: nil, At: now}); err != nil {
		t.Fatal(err)
	}
	racing.Fail(now)
	if err = s.Save(ctx, &racing, uuid.NewV7()); !errors.Is(err, quota.ErrConflict) {
		t.Fatalf("a second first save: %v", err)
	}
	got, _ := s.Get(ctx, key)
	if got.Failures() != 1 {
		t.Fatalf("failures %d", got.Failures())
	}
	for range quota.MaxFailures - 1 {
		got.Fail(now)
	}
	if err = s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	locked, err := s.LockedUntil(ctx, [][]byte{key, []byte("other")}, now)
	if err != nil || !locked[string(key)].Equal(now.Add(quota.LockFor)) || len(locked) != 1 {
		t.Fatalf("%v %v", locked, err)
	}
	if after, _ := s.LockedUntil(ctx, [][]byte{key}, now.Add(quota.LockFor+time.Second)); len(after) != 0 {
		t.Fatal("a lapsed lock is listed")
	}
}
