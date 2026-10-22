package account_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
)

// TestPGStore checks that the Postgres store behaves as account.Store
// promises.
func TestPGStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pgNow := time.Now().UTC().Truncate(time.Microsecond)
	fresh := func(t *testing.T, s account.Store) account.Account {
		t.Helper()
		a, err := account.New(uuid.NewV7(), "Linh", pgNow)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}

	t.Run("create then get", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		got, err := s.Get(ctx, a.ID())
		if err != nil || got.ID() != a.ID() || got.DisplayName() != "Linh" || got.Status(pgNow) != account.StatusActive ||
			got.WriteID() != a.WriteID() || !got.CreatedAt().Equal(pgNow) {
			t.Fatalf("got %+v %v", got, err)
		}
	})

	t.Run("create twice keeps the first", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		again, _ := account.New(a.ID(), "Other", pgNow)
		if err := s.Create(ctx, again); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Get(ctx, a.ID()); got.DisplayName() != "Linh" {
			t.Fatalf("name %q", got.DisplayName())
		}
	})

	t.Run("get unknown", func(t *testing.T) {
		t.Parallel()
		if _, err := newStore(t).Get(ctx, uuid.NewV7()); !errors.Is(err, account.ErrNotFound) {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("save sets the write id", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		a.SetDisplayName("Minh", pgNow)
		w := uuid.NewV7()
		if err := s.Save(ctx, &a, w); err != nil || a.WriteID() != w {
			t.Fatalf("save: %v, write id %s", err, a.WriteID())
		}
		if got, _ := s.Get(ctx, a.ID()); got.DisplayName() != "Minh" || got.WriteID() != w {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("a stale write conflicts", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		b, _ := s.Get(ctx, a.ID())
		a.SetDisplayName("First", pgNow)
		if err := s.Save(ctx, &a, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		b.SetDisplayName("Second", pgNow)
		if err := s.Save(ctx, &b, uuid.NewV7()); !errors.Is(err, account.ErrConflict) {
			t.Fatalf("stale save: %v", err)
		}
	})

	t.Run("a retried write with the same id succeeds", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		a.SetDisplayName("Minh", pgNow)
		retry := a
		w := uuid.NewV7()
		if err := s.Save(ctx, &a, w); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		for range 2 {
			if err := s.Delete(ctx, a.ID()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Get(ctx, a.ID()); !errors.Is(err, account.ErrNotFound) {
			t.Fatalf("after delete: %v", err)
		}
	})
}
