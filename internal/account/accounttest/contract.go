// Package accounttest holds the in-memory account store and the contract
// suite every account.Store implementation must pass.
package accounttest

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

// RunStoreContract checks that a store behaves as account.Store promises.
// newStore returns a fresh store and the Querier to pass to it.
func RunStoreContract(t *testing.T, newStore func(t *testing.T) (account.Store, postgres.Querier)) {
	ctx := context.Background()
	// Postgres keeps microseconds; compare at that precision.
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Run("insert then get", func(t *testing.T) {
		s, q := newStore(t)
		a, _ := account.New("Linh", now)
		if err := s.Insert(ctx, q, &a); err != nil {
			t.Fatal(err)
		}
		if a.ID() == (uuid.UUID{}) || a.ID()[6]>>4 != 7 || a.CreatedAt().IsZero() {
			t.Fatalf("after insert: id %s (v%d), created %v", a.ID(), a.ID()[6]>>4, a.CreatedAt())
		}
		got, err := s.Get(ctx, q, a.ID())
		if err != nil {
			t.Fatal(err)
		}
		if got.ID() != a.ID() || got.DisplayName() != "Linh" || got.Status(now) != account.StatusActive ||
			!got.CreatedAt().Equal(a.CreatedAt()) {
			t.Fatalf("got %+v, want %+v", got, a)
		}
	})

	t.Run("insert keeps an empty name", func(t *testing.T) {
		s, q := newStore(t)
		a, _ := account.New("", now)
		if err := s.Insert(ctx, q, &a); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, q, a.ID())
		if err != nil || got.DisplayName() != "" {
			t.Fatalf("%q %v", got.DisplayName(), err)
		}
	})

	t.Run("get unknown", func(t *testing.T) {
		s, q := newStore(t)
		if _, err := s.Get(ctx, q, uuid.NewV7()); !errors.Is(err, account.ErrNotFound) {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("get for update reads like get", func(t *testing.T) {
		s, q := newStore(t)
		a, _ := account.New("Linh", now)
		if err := s.Insert(ctx, q, &a); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetForUpdate(ctx, q, a.ID())
		if err != nil || got.ID() != a.ID() || got.DisplayName() != "Linh" {
			t.Fatalf("%+v %v", got, err)
		}
		if _, err := s.GetForUpdate(ctx, q, uuid.NewV7()); !errors.Is(err, account.ErrNotFound) {
			t.Fatalf("unknown: %v", err)
		}
	})

	t.Run("update round-trips suspension", func(t *testing.T) {
		s, q := newStore(t)
		a, _ := account.New("Linh", now)
		s.Insert(ctx, q, &a)
		until := now.Add(48 * time.Hour)
		later := now.Add(time.Minute)
		if err := a.Suspend("scam reports", &until, later); err != nil {
			t.Fatal(err)
		}
		if err := s.Update(ctx, q, a); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, q, a.ID())
		if err != nil {
			t.Fatal(err)
		}
		if got.Status(later) != account.StatusSuspended || got.SuspendReason() != "scam reports" ||
			got.SuspendedUntil() == nil || !got.SuspendedUntil().Equal(until) || !got.UpdatedAt().Equal(later) {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("update unknown", func(t *testing.T) {
		s, q := newStore(t)
		a := account.Restore(uuid.NewV7(), "x", account.StatusActive, nil, "", now, now)
		if err := s.Update(ctx, q, a); !errors.Is(err, account.ErrNotFound) {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("ids sort by creation", func(t *testing.T) {
		s, q := newStore(t)
		a, _ := account.New("a", now)
		b, _ := account.New("b", now)
		s.Insert(ctx, q, &a)
		s.Insert(ctx, q, &b)
		if a.ID() == b.ID() || a.ID().Compare(b.ID()) >= 0 {
			t.Fatalf("a %s, b %s", a.ID(), b.ID())
		}
	})
}
