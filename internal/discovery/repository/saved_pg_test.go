package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func newAccount(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

// listings inserts n listings, 20 per new seller (the cap), and returns
// their IDs.
func listings(t *testing.T, db *pgxpool.Pool, n int) []uuid.UUID {
	t.Helper()
	store := catalogrepo.NewListings(db)
	out := make([]uuid.UUID, 0, n)
	var seller uuid.UUID
	for i := range n {
		if i%catalogdomain.MaxOnSale == 0 {
			seller = newAccount(t, db)
		}
		l, err := catalogdomain.NewListing(uuid.NewV7(), seller, catalogdomain.Draft{
			Title: "Xe đạp", Price: 1_500_000, CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed,
			CommuneCode: "00004", Photos: []uuid.UUID{uuid.NewV7()},
		}, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true}, now)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Insert(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l.ID())
	}
	return out
}

func TestSaves(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSaves(db)
	ctx := context.Background()
	me := newAccount(t, db)
	ls := listings(t, db, 3)
	for range 2 {
		if err := s.Save(ctx, uuid.NewV7(), me, ls[0], 1_500_000, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(ctx, uuid.NewV7(), me, ls[1], 1_400_000, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx, me, page.Request{})
	if err != nil || len(got) != 2 || got[0].ListingID != ls[1] || got[0].Price != 1_400_000 {
		t.Fatalf("%+v %v", got, err)
	}
	if err = s.Remove(ctx, me, ls[0]); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.List(ctx, me, page.Request{}); len(got) != 1 {
		t.Fatalf("after remove: %+v", got)
	}
}

func TestSaveLimit(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSaves(db)
	ctx := context.Background()
	me := newAccount(t, db)
	ls := listings(t, db, domain.MaxSaved+1)
	for _, l := range ls[:domain.MaxSaved] {
		if err := s.Save(ctx, uuid.NewV7(), me, l, 1, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(ctx, uuid.NewV7(), me, ls[domain.MaxSaved], 1, now); !errors.Is(err, domain.ErrLimit) {
		t.Fatalf("the 501st: %v", err)
	}
	if err := s.Save(ctx, uuid.NewV7(), me, ls[0], 1, now); err != nil {
		t.Fatalf("saving one of the 500 again: %v", err)
	}
}
