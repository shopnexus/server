package credentials_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

// TestPGStore checks that the Postgres store behaves as credentials.Store
// promises. newStore also returns a function that creates an account to own
// rows.
func TestPGStore(t *testing.T) {
	t.Parallel()
	newStore := func(t *testing.T) (credentials.Store, func() uuid.UUID) {
		t.Helper()
		pool := pgtest.New(t)
		newAccount := func() uuid.UUID {
			id := uuid.NewV7()
			if _, err := pool.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`,
				pgx.StrictNamedArgs{"id": id}); err != nil {
				t.Fatal(err)
			}
			return id
		}
		return credentials.NewPGStore(pool), newAccount
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	phone, _ := contact.ParsePhone("0901234567")
	email, _ := contact.ParseEmail("linh@gmail.com")
	fresh := func(t *testing.T, s credentials.Store, acc uuid.UUID, a contact.Address) credentials.Credentials {
		t.Helper()
		c := credentials.New(acc, credentials.NewIdentity(uuid.NewV7(), a, now), now)
		if err := s.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		return c
	}

	t.Run("create, get and find", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newStore(t)
		acc := newAccount()
		c := fresh(t, s, acc, phone)
		got, err := s.Get(ctx, acc)
		if err != nil || got.WriteID() != acc || len(got.Identities()) != 1 || got.Identities()[0].Value() != "+84901234567" {
			t.Fatalf("got %+v %v", got, err)
		}
		ref, err := s.FindIdentity(ctx, credentials.Phone, "+84901234567")
		if err != nil || ref != (credentials.IdentityRef{AccountID: acc, IdentityID: c.Identities()[0].ID()}) {
			t.Fatalf("find: %+v %v", ref, err)
		}
		if _, err := s.FindIdentity(ctx, credentials.Email, "+84901234567"); !errors.Is(err, credentials.ErrNotFound) {
			t.Fatalf("find by the wrong kind: %v", err)
		}
	})

	t.Run("create twice is a no-op", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newStore(t)
		c := fresh(t, s, newAccount(), phone)
		if err := s.Create(ctx, c); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})

	t.Run("another account's identity is taken", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newStore(t)
		first := fresh(t, s, newAccount(), phone)
		second := credentials.New(newAccount(), credentials.NewIdentity(uuid.NewV7(), phone, now), now)
		if err := s.Create(ctx, second); !errors.Is(err, credentials.ErrTaken) {
			t.Fatalf("duplicate: %v", err)
		}
		if got, _ := s.Get(ctx, first.AccountID()); !got.Identities()[0].LastUsedAt().Equal(now) {
			t.Fatal("the losing create touched the owner's identity")
		}
		if _, err := s.Get(ctx, second.AccountID()); !errors.Is(err, credentials.ErrNotFound) {
			t.Fatalf("the losing create left a row: %v", err)
		}
	})

	t.Run("save, conflict and retry", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newStore(t)
		acc := newAccount()
		fresh(t, s, acc, email)
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
		if err := s.Save(ctx, &b, uuid.NewV7()); !errors.Is(err, credentials.ErrConflict) {
			t.Fatalf("stale: %v", err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})

	t.Run("get unknown", func(t *testing.T) {
		t.Parallel()
		s, _ := newStore(t)
		if _, err := s.Get(ctx, uuid.NewV7()); !errors.Is(err, credentials.ErrNotFound) {
			t.Fatalf("err %v", err)
		}
	})
}
