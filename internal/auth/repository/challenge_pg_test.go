package repository_test

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var ip = netip.MustParseAddr("203.0.113.7")

// TestPGChallenges checks that the Postgres store behaves as
// repository.Challenges promises.
func TestPGChallenges(t *testing.T) {
	t.Parallel()
	newStore := func(t *testing.T) repository.Challenges {
		t.Helper()
		return repository.NewChallenges(pgtest.New(t))
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	phone, _ := contact.ParsePhone("0901234567")
	other, _ := contact.ParsePhone("0907654321")
	create := func(t *testing.T, s repository.Challenges, dest contact.Address, purpose domain.Purpose, at time.Time) domain.Challenge {
		t.Helper()
		c := domain.NewChallenge(uuid.NewV7(), purpose, dest, ip, []byte("hash-"+at.String()), at)
		if err := s.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		return c
	}

	// Save leaves the challenge carrying the write ID it wrote, so saving it
	// again succeeds.
	t.Run("save twice", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		c := create(t, s, phone, domain.PurposeLogin, now)
		for range 2 {
			_ = c.Verify([]byte("wrong"), now)
			if err := s.Save(ctx, &c, uuid.NewV7()); err != nil {
				t.Fatalf("save: %v", err)
			}
		}
	})

	t.Run("create then get, twice is a no-op", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		c := create(t, s, phone, domain.PurposeLogin, now)
		if err := s.Create(ctx, domain.NewChallenge(c.ID(), domain.PurposeLogin, other, ip, []byte("x"), now)); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, c.ID())
		if err != nil || got.Destination() != phone || got.IP() != ip || !bytes.Equal(got.CodeHash(), c.CodeHash()) ||
			got.Attempts() != 0 || !got.CreatedAt().Equal(now) || !got.ExpiresAt().Equal(now.Add(domain.CodeTTL)) ||
			got.ConsumedAt() != nil || got.WriteID() != c.ID() {
			t.Fatalf("got %+v %v", got, err)
		}
	})

	t.Run("get unknown", func(t *testing.T) {
		t.Parallel()
		if _, err := newStore(t).Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("save, conflict and retry", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		c := create(t, s, phone, domain.PurposeLogin, now)
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
		if err := s.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("stale save: %v", err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry with the same write id: %v", err)
		}
	})

	t.Run("newest per destination and purpose", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		first := create(t, s, phone, domain.PurposeLogin, now)
		second := create(t, s, phone, domain.PurposeLogin, now.Add(time.Second))
		elsewhere := create(t, s, other, domain.PurposeLogin, now.Add(2*time.Second))
		create(t, s, phone, domain.Purpose("reset_password"), now.Add(3*time.Second))
		for _, c := range []struct {
			ch   domain.Challenge
			want bool
		}{{first, false}, {second, true}, {elsewhere, true}} {
			if got, err := s.IsNewest(ctx, c.ch); err != nil || got != c.want {
				t.Fatalf("%s: %v %v", c.ch.CreatedAt(), got, err)
			}
		}
	})
}
