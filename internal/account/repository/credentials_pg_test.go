package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// TestPGCredentials checks that the Postgres store behaves as repository.Credentials
// promises. newCredentials also returns the store's pool.
func TestPGCredentials(t *testing.T) {
	t.Parallel()
	newCredentials := func(t *testing.T) (repository.Credentials, *pgxpool.Pool) {
		t.Helper()
		pool := pgtest.New(t)
		return repository.NewCredentials(pool), pool
	}
	ctx := context.Background()
	phone, _ := contact.ParsePhone("0901234567")
	other, _ := contact.ParsePhone("0912345678")
	newAccount := func(t *testing.T) domain.Account {
		t.Helper()
		a, err := domain.New(uuid.NewV7(), "", now)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	fresh := func(t *testing.T, s repository.Credentials, a contact.Address) domain.Credentials {
		t.Helper()
		acc := newAccount(t)
		c := domain.NewCredentials(acc.ID(), domain.NewIdentity(uuid.NewV7(), a, now), now)
		if err := s.SignUp(ctx, acc, c); err != nil {
			t.Fatal(err)
		}
		return c
	}

	email, _ := contact.ParseEmail("linh@example.com")

	// Save writes an identity Add gave the credentials.
	t.Run("save adds an email", func(t *testing.T) {
		t.Parallel()
		s, _ := newCredentials(t)
		c := fresh(t, s, phone)
		if err := c.Add(domain.NewIdentity(uuid.NewV7(), email, now)); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &c, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		ref, err := s.FindIdentity(ctx, domain.KindEmail, "linh@example.com")
		if err != nil || ref.AccountID != c.AccountID() {
			t.Fatalf("%+v %v", ref, err)
		}
	})

	// An email another account holds is ErrIdentityTaken and writes nothing.
	t.Run("save an email another account holds", func(t *testing.T) {
		t.Parallel()
		s, _ := newCredentials(t)
		fresh(t, s, email)
		c := fresh(t, s, phone)
		if err := c.Add(domain.NewIdentity(uuid.NewV7(), email, now)); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &c, uuid.NewV7()); !errors.Is(err, domain.ErrIdentityTaken) {
			t.Fatalf("%v", err)
		}
		back, err := s.Get(ctx, c.AccountID())
		if err != nil || len(back.Identities()) != 1 {
			t.Fatalf("%+v %v", back.Identities(), err)
		}
	})

	// Save leaves the credentials carrying the write ID they wrote, so saving
	// them again succeeds.
	t.Run("save twice", func(t *testing.T) {
		t.Parallel()
		s, _ := newCredentials(t)
		c := fresh(t, s, phone)
		for n := range 2 {
			if err := c.MarkUsed(c.Identities()[0].ID(), now.Add(time.Duration(n+1)*time.Minute)); err != nil {
				t.Fatal(err)
			}
			if err := s.Save(ctx, &c, uuid.NewV7()); err != nil {
				t.Fatalf("save %d: %v", n, err)
			}
		}
	})

	t.Run("sign up, get and find", func(t *testing.T) {
		t.Parallel()
		s, _ := newCredentials(t)
		c := fresh(t, s, phone)
		acc := c.AccountID()
		got, err := s.Get(ctx, acc)
		if err != nil || got.WriteID() != acc || len(got.Identities()) != 1 || got.Identities()[0].Value() != "+84901234567" {
			t.Fatalf("got %+v %v", got, err)
		}
		ref, err := s.FindIdentity(ctx, domain.KindPhone, "+84901234567")
		if err != nil || ref != (domain.IdentityRef{AccountID: acc, IdentityID: c.Identities()[0].ID()}) {
			t.Fatalf("find: %+v %v", ref, err)
		}
		if _, err := s.FindIdentity(ctx, domain.Kind("email"), "+84901234567"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("find by the wrong kind: %v", err)
		}
	})

	t.Run("sign up twice is a no-op", func(t *testing.T) {
		t.Parallel()
		s, _ := newCredentials(t)
		acc := newAccount(t)
		c := domain.NewCredentials(acc.ID(), domain.NewIdentity(uuid.NewV7(), phone, now), now)
		for n := range 2 {
			if err := s.SignUp(ctx, acc, c); err != nil {
				t.Fatalf("create %d: %v", n, err)
			}
		}
	})

	t.Run("another account's identity is taken", func(t *testing.T) {
		t.Parallel()
		s, pool := newCredentials(t)
		first := fresh(t, s, phone)
		loser := newAccount(t)
		second := domain.NewCredentials(loser.ID(), domain.NewIdentity(uuid.NewV7(), phone, now), now)
		if err := s.SignUp(ctx, loser, second); !errors.Is(err, domain.ErrIdentityTaken) {
			t.Fatalf("duplicate: %v", err)
		}
		if got, _ := s.Get(ctx, first.AccountID()); !got.Identities()[0].LastUsedAt().Equal(now) {
			t.Fatal("the losing sign-up touched the owner's identity")
		}
		if _, err := s.Get(ctx, second.AccountID()); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("the losing sign-up left credentials: %v", err)
		}
		if _, err := repository.NewAccounts(pool).Get(ctx, loser.ID()); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("the losing sign-up left an account: %v", err)
		}
	})

	t.Run("save, conflict and retry", func(t *testing.T) {
		t.Parallel()
		s, _ := newCredentials(t)
		c := fresh(t, s, other)
		acc := c.AccountID()
		a, _ := s.Get(ctx, acc)
		b, _ := s.Get(ctx, acc)
		a.MarkUsed(a.Identities()[0].ID(), now.Add(time.Hour))
		retry := a
		w := uuid.NewV7()
		if err := s.Save(ctx, &a, w); err != nil || a.WriteID() != w {
			t.Fatalf("save: %v", err)
		}
		if got, _ := s.Get(ctx, acc); !got.Identities()[0].LastUsedAt().Equal(now.Add(time.Hour)) {
			t.Fatalf("last used %v", got.Identities()[0].LastUsedAt())
		}
		b.MarkUsed(b.Identities()[0].ID(), now)
		if err := s.Save(ctx, &b, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("stale: %v", err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})

	t.Run("get unknown", func(t *testing.T) {
		t.Parallel()
		s, _ := newCredentials(t)
		if _, err := s.Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("err %v", err)
		}
	})
}
