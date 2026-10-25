package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

// TestPGQuotas checks that the Postgres store behaves as repository.Quotas
// promises.
func TestPGQuotas(t *testing.T) {
	t.Parallel()
	newStore := func(t *testing.T) repository.Quotas {
		t.Helper()
		return repository.NewQuotas(pgtest.New(t))
	}
	ctx := context.Background()
	pgNow := time.Now().UTC().Truncate(time.Microsecond)
	rule := domain.Rule{Window: time.Hour, Max: 10}

	t.Run("a missing quota is empty", func(t *testing.T) {
		t.Parallel()
		q, err := newStore(t).Get(ctx, "ip:1.2.3.4")
		if err != nil || q.Key() != "ip:1.2.3.4" || len(q.SentAt()) != 0 || q.WriteID() != (uuid.UUID{}) {
			t.Fatalf("%+v %v", q, err)
		}
	})

	// Save leaves the quota carrying the write ID it wrote, so saving it
	// again succeeds.
	t.Run("save twice", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		q, _ := s.Get(ctx, "ip:5.6.7.8")
		for n := range 2 {
			q.Admit(pgNow.Add(time.Duration(n)*time.Second), rule)
			if err := s.Save(ctx, &q, uuid.NewV7()); err != nil {
				t.Fatalf("save %d: %v", n, err)
			}
		}
	})

	t.Run("save then get", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		q, _ := s.Get(ctx, "ip:1.2.3.4")
		q.Admit(pgNow, rule)
		w := uuid.NewV7()
		if err := s.Save(ctx, &q, w); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get(ctx, "ip:1.2.3.4")
		if len(got.SentAt()) != 1 || !got.SentAt()[0].Equal(pgNow) || got.WriteID() != w {
			t.Fatalf("%+v", got)
		}
	})

	t.Run("two first saves conflict", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a, _ := s.Get(ctx, "dest:x")
		b, _ := s.Get(ctx, "dest:x")
		a.Admit(pgNow, rule)
		b.Admit(pgNow, rule)
		if err := s.Save(ctx, &a, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &b, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("second first save: %v", err)
		}
	})

	t.Run("a stale save conflicts and a retry succeeds", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		q, _ := s.Get(ctx, "dest:x")
		q.Admit(pgNow, rule)
		s.Save(ctx, &q, uuid.NewV7())
		a, _ := s.Get(ctx, "dest:x")
		b, _ := s.Get(ctx, "dest:x")
		a.Admit(pgNow.Add(time.Second), rule)
		b.Admit(pgNow.Add(time.Second), rule)
		w := uuid.NewV7()
		retry := a
		if err := s.Save(ctx, &a, w); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &b, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("stale: %v", err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})
}
