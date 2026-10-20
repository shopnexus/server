// Package otptest holds in-memory OTP stores and the contract suites every
// implementation must pass.
package otptest

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/contact"
)

var ip = netip.MustParseAddr("203.0.113.7")

// RunChallengeContract checks that a store behaves as otp.ChallengeStore
// promises.
func RunChallengeContract(t *testing.T, newStore func(t *testing.T) otp.ChallengeStore) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	phone, _ := contact.ParsePhone("0901234567")
	other, _ := contact.ParsePhone("0907654321")
	create := func(t *testing.T, s otp.ChallengeStore, dest contact.Address, purpose otp.Purpose, at time.Time) otp.Challenge {
		t.Helper()
		c := otp.New(uuid.NewV7(), purpose, dest, ip, []byte("hash-"+at.String()), at)
		if err := s.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		return c
	}

	t.Run("create then get, twice is a no-op", func(t *testing.T) {
		s := newStore(t)
		c := create(t, s, phone, otp.PurposeLogin, now)
		if err := s.Create(ctx, otp.New(c.ID(), otp.PurposeLogin, other, ip, []byte("x"), now)); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, c.ID())
		if err != nil || got.Destination() != phone || got.IP() != ip || !bytes.Equal(got.CodeHash(), c.CodeHash()) ||
			got.Attempts() != 0 || !got.CreatedAt().Equal(now) || !got.ExpiresAt().Equal(now.Add(otp.CodeTTL)) ||
			got.ConsumedAt() != nil || got.WriteID() != c.ID() {
			t.Fatalf("got %+v %v", got, err)
		}
	})

	t.Run("get unknown", func(t *testing.T) {
		if _, err := newStore(t).Get(ctx, uuid.NewV7()); !errors.Is(err, otp.ErrNotFound) {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("save, conflict and retry", func(t *testing.T) {
		s := newStore(t)
		c := create(t, s, phone, otp.PurposeLogin, now)
		stale := c
		c.Verify([]byte("wrong"), now)
		retry := c
		w := uuid.NewV7()
		if err := s.Save(ctx, &c, w); err != nil || c.WriteID() != w {
			t.Fatalf("save: %v", err)
		}
		if got, _ := s.Get(ctx, c.ID()); got.Attempts() != 1 {
			t.Fatalf("attempts %d", got.Attempts())
		}
		stale.Verify([]byte("wrong"), now)
		if err := s.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, otp.ErrConflict) {
			t.Fatalf("stale save: %v", err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry with the same write id: %v", err)
		}
	})

	t.Run("newest per destination and purpose", func(t *testing.T) {
		s := newStore(t)
		first := create(t, s, phone, otp.PurposeLogin, now)
		second := create(t, s, phone, otp.PurposeLogin, now.Add(time.Second))
		elsewhere := create(t, s, other, otp.PurposeLogin, now.Add(2*time.Second))
		create(t, s, phone, otp.Purpose("reset_password"), now.Add(3*time.Second))
		for _, c := range []struct {
			ch   otp.Challenge
			want bool
		}{{first, false}, {second, true}, {elsewhere, true}} {
			if got, err := s.IsNewest(ctx, c.ch); err != nil || got != c.want {
				t.Fatalf("%s: %v %v", c.ch.CreatedAt(), got, err)
			}
		}
	})
}

// RunQuotaContract checks that a store behaves as otp.QuotaStore promises.
func RunQuotaContract(t *testing.T, newStore func(t *testing.T) otp.QuotaStore) {
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
