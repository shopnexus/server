package repository_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// offered stores a new buyer's deal on a listing of seller's with the
// buyer's offer made that long after now.
func offered(t *testing.T, db *pgxpool.Pool, s repository.Deals, seller uuid.UUID, after time.Duration) domain.Deal {
	t.Helper()
	d := inserted(t, db, s, catalogtest.Listing(t, db, seller), accounttest.New(t, db), seller)
	if err := d.Offer(domain.Buyer, 1_000_000, onSale, now.Add(after)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(context.Background(), &d, uuid.NewV7(), uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOpenOffers(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	seller := accounttest.New(t, db)
	a := offered(t, db, s, seller, time.Minute)
	b := offered(t, db, s, seller, 3*time.Minute)
	c := offered(t, db, s, seller, 2*time.Minute)

	// Left out: a deal the seller countered, an agreed one, another seller's.
	countered := offered(t, db, s, seller, 4*time.Minute)
	if err := countered.Offer(domain.Seller, 1_200_000, onSale, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &countered, uuid.NewV7(), uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	pendingDeal(t, db, s, accounttest.New(t, db), seller)
	offered(t, db, s, accounttest.New(t, db), 6*time.Minute)

	ids := func(ds []domain.Deal) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(ds))
		for i := range ds {
			out = append(out, ds[i].ID())
		}
		return out
	}
	want := func(name string, p page.Request, deals ...domain.Deal) {
		t.Helper()
		got, err := s.OpenOffers(ctx, seller, p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != len(deals) {
			t.Fatalf("%s: got %v, want %d deals", name, ids(got), len(deals))
		}
		for i := range deals {
			if got[i].ID() != deals[i].ID() {
				t.Fatalf("%s: got %v, want %v", name, ids(got), ids(deals))
			}
		}
	}
	// Newest offer first, whatever the deals' own order.
	want("first page", page.Request{Limit: 1}, b, c)
	want("after b", page.Request{Cursor: b.ID(), Limit: 1}, c, a)
	want("after c", page.Request{Cursor: c.ID()}, a)
	if n, err := s.CountOpenOffers(ctx, seller); err != nil || n != 3 {
		t.Fatalf("count: %d %v", n, err)
	}

	// A cursor deal whose offer was answered pages from the newest again.
	if err := c.Decline(domain.Seller, now.Add(7*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &c, uuid.NewV7(), uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	want("after the answered c", page.Request{Cursor: c.ID()}, b, a)
	if n, err := s.CountOpenOffers(ctx, seller); err != nil || n != 2 {
		t.Fatalf("count after the decline: %d %v", n, err)
	}
}
