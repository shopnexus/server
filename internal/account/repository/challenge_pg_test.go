package repository_test

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
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
	at := time.Now().UTC().Truncate(time.Microsecond)
	addr, _ := contact.ParseEmail("linh@example.com")
	other, _ := contact.ParseEmail("linh2@example.com")
	create := func(t *testing.T, s repository.Challenges, dest contact.Address, purpose domain.Purpose, when time.Time) domain.Challenge {
		t.Helper()
		c := domain.NewChallenge(uuid.NewV7(), purpose, dest, nil, ip, []byte("hash-"+when.String()), when)
		if _, err := s.Create(ctx, c, uuid.Nil()); err != nil {
			t.Fatal(err)
		}
		return c
	}

	// An add_identity challenge keeps the account that asked for it.
	t.Run("add identity names the account", func(t *testing.T) {
		t.Parallel()
		db := pgtest.New(t)
		s := repository.NewChallenges(db)
		acc := accounttest.New(t, db)
		c := domain.NewChallenge(uuid.NewV7(), domain.PurposeAddIdentity, addr, &acc, ip, []byte{1}, at)
		if _, err := s.Create(ctx, c, uuid.Nil()); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, c.ID())
		if err != nil || got.AccountID() == nil || *got.AccountID() != acc || got.Purpose() != domain.PurposeAddIdentity {
			t.Fatalf("%+v %v", got, err)
		}
	})

	// Save leaves the challenge carrying the write ID it wrote, so saving it
	// again succeeds.
	t.Run("save twice", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		c := create(t, s, addr, domain.PurposeSignIn, at)
		for range 2 {
			_ = c.Verify([]byte("wrong"), at)
			if err := s.Save(ctx, &c, uuid.NewV7()); err != nil {
				t.Fatalf("save: %v", err)
			}
		}
	})

	t.Run("create then get, twice is a no-op", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		c := create(t, s, addr, domain.PurposeSignIn, at)
		if _, err := s.Create(ctx, domain.NewChallenge(c.ID(), domain.PurposeSignIn, other, nil, ip, []byte("x"), at), uuid.Nil()); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, c.ID())
		if err != nil || got.Destination() != addr || got.IP() != ip || !bytes.Equal(got.CodeHash(), c.CodeHash()) ||
			got.Attempts() != 0 || !got.CreatedAt().Equal(at) || !got.ExpiresAt().Equal(at.Add(domain.CodeTTL)) ||
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
		c := create(t, s, addr, domain.PurposeSignIn, at)
		stale := c
		c.Verify([]byte("wrong"), at)
		retry := c
		w := uuid.NewV7()
		if err := s.Save(ctx, &c, w); err != nil || c.WriteID() != w {
			t.Fatalf("save: %v", err)
		}
		if got, _ := s.Get(ctx, c.ID()); got.Attempts() != 1 {
			t.Fatalf("attempts %d", got.Attempts())
		}
		stale.Verify([]byte("wrong"), at)
		if err := s.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("stale save: %v", err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry with the same write id: %v", err)
		}
	})

	t.Run("newest per destination and purpose", func(t *testing.T) {
		t.Parallel()
		db := pgtest.New(t)
		s := repository.NewChallenges(db)
		first := create(t, s, addr, domain.PurposeSignIn, at)
		second := create(t, s, addr, domain.PurposeSignIn, at.Add(time.Second))
		elsewhere := create(t, s, other, domain.PurposeSignIn, at.Add(2*time.Second))
		acc := accounttest.New(t, db)
		add := domain.NewChallenge(uuid.NewV7(), domain.PurposeAddIdentity, addr, &acc, ip, []byte("x"), at.Add(3*time.Second))
		if _, err := s.Create(ctx, add, uuid.Nil()); err != nil {
			t.Fatal(err)
		}
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
