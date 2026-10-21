package otp_test

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
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

var ip = netip.MustParseAddr("203.0.113.7")

// TestPGChallenges checks that the Postgres store behaves as
// otp.ChallengeStore promises.
func TestPGChallenges(t *testing.T) {
	newStore := func(t *testing.T) otp.ChallengeStore { return otp.NewChallengePGStore(pgtest.New(t)) }
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
