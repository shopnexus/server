package otp_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

// TestPGQuotas checks that the Postgres store behaves as otp.QuotaStore
// promises.
func TestPGQuotas(t *testing.T) {
	newStore := func(t *testing.T) otp.QuotaStore { return otp.NewQuotaPGStore(pgtest.New(t)) }
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	rule := otp.Rule{Window: time.Hour, Max: 10}

	t.Run("a missing quota is empty", func(t *testing.T) {
		q, err := newStore(t).Get(ctx, "ip:1.2.3.4")
		if err != nil || q.Key() != "ip:1.2.3.4" || len(q.SentAt()) != 0 || q.WriteID() != (uuid.UUID{}) {
			t.Fatalf("%+v %v", q, err)
		}
	})

	t.Run("save then get", func(t *testing.T) {
		s := newStore(t)
		q, _ := s.Get(ctx, "ip:1.2.3.4")
		q.Admit(now, rule)
		w := uuid.NewV7()
		if err := s.Save(ctx, &q, w); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get(ctx, "ip:1.2.3.4")
		if len(got.SentAt()) != 1 || !got.SentAt()[0].Equal(now) || got.WriteID() != w {
			t.Fatalf("%+v", got)
		}
	})

	t.Run("two first saves conflict", func(t *testing.T) {
		s := newStore(t)
		a, _ := s.Get(ctx, "dest:x")
		b, _ := s.Get(ctx, "dest:x")
		a.Admit(now, rule)
		b.Admit(now, rule)
		if err := s.Save(ctx, &a, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &b, uuid.NewV7()); !errors.Is(err, otp.ErrConflict) {
			t.Fatalf("second first save: %v", err)
		}
	})

	t.Run("a stale save conflicts and a retry succeeds", func(t *testing.T) {
		s := newStore(t)
		q, _ := s.Get(ctx, "dest:x")
		q.Admit(now, rule)
		s.Save(ctx, &q, uuid.NewV7())
		a, _ := s.Get(ctx, "dest:x")
		b, _ := s.Get(ctx, "dest:x")
		a.Admit(now.Add(time.Second), rule)
		b.Admit(now.Add(time.Second), rule)
		w := uuid.NewV7()
		retry := a
		if err := s.Save(ctx, &a, w); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &b, uuid.NewV7()); !errors.Is(err, otp.ErrConflict) {
			t.Fatalf("stale: %v", err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})
}
