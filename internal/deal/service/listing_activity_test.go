package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/service"
)

// Each of the seller's listings lists its buyers, oldest deal first, and
// the order its card shows: the shipped one, else the pending order
// requested first. A cancelled order shows nothing, and another seller's
// listing is left out.
func TestListingActivity(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, stranger := e.person(t), e.person(t)
	first, second, third := e.person(t), e.person(t), e.person(t)
	twoPending, shipped, talked, cancelled := catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller),
		catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller)
	foreign := catalogtest.Listing(t, e.db, stranger)

	early := e.pending(t, first, twoPending)
	e.pending(t, second, twoPending)
	sent := e.shipped(t, third, seller, shipped)
	e.open(t, first, talked)
	gone := e.pending(t, second, cancelled)
	must(t)(e.deals.CancelOrder(ctx, seller, gone.Order.ID, ""))
	e.open(t, first, foreign)

	got, err := e.deals.ListingActivity(ctx, seller, []uuid.UUID{twoPending, shipped, talked, cancelled, foreign, uuid.NewV7()})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("%d listings: %+v", len(got), got)
	}
	buyers := func(id uuid.UUID) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(got[id].Buyers))
		for _, b := range got[id].Buyers {
			if b.Since.IsZero() {
				t.Fatalf("a buyer without a time: %+v", b)
			}
			out = append(out, b.ID)
		}
		return out
	}
	for id, want := range map[uuid.UUID][]uuid.UUID{
		twoPending: {first, second}, shipped: {third}, talked: {first}, cancelled: {second},
	} {
		if got := buyers(id); len(got) != len(want) || got[0] != want[0] || got[len(got)-1] != want[len(want)-1] {
			t.Fatalf("buyers of %s: %v, want %v", id, got, want)
		}
	}
	if o := got[twoPending].Order; o == nil ||
		*o != (service.ListingOrderView{DealID: early.ID, OrderID: early.Order.ID, Status: domain.OrderPending}) {
		t.Fatalf("two pending orders show %+v, want the first", o)
	}
	if o := got[shipped].Order; o == nil || *o != (service.ListingOrderView{DealID: sent.ID, OrderID: sent.Order.ID, Status: domain.OrderShipped}) {
		t.Fatalf("a shipped order shows %+v", o)
	}
	if got[talked].Order != nil || got[cancelled].Order != nil {
		t.Fatalf("no open order shows %+v and %+v", got[talked].Order, got[cancelled].Order)
	}
}

// A deal whose order was cancelled and asked for again holds two orders
// and lists its buyer once, with the new order. The database refuses a
// second open order on one deal, so no deal shows two.
func TestListingActivityOneRowPerDeal(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	first := e.pending(t, buyer, listing)
	must(t)(e.deals.CancelOrder(ctx, buyer, first.Order.ID, ""))
	again, err := e.deals.RequestCod(ctx, buyer, first.ID, 1_500_000, e.address(t, buyer), "")
	if err != nil {
		t.Fatal(err)
	}
	var orders int
	if err = e.db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE deal_id = @deal`, pgx.StrictNamedArgs{"deal": first.ID}).Scan(&orders); err != nil || orders != 2 {
		t.Fatalf("the deal holds %d orders: %v", orders, err)
	}
	got, err := e.deals.ListingActivity(ctx, seller, []uuid.UUID{listing})
	if err != nil {
		t.Fatal(err)
	}
	a := got[listing]
	if len(a.Buyers) != 1 || a.Buyers[0].ID != buyer {
		t.Fatalf("buyers: %+v", a.Buyers)
	}
	if a.Order == nil || *a.Order != (service.ListingOrderView{DealID: first.ID, OrderID: again.Order.ID, Status: domain.OrderPending}) {
		t.Fatalf("order: %+v, want %s", a.Order, again.Order.ID)
	}

	_, err = e.db.Exec(ctx, `INSERT INTO orders
SELECT (jsonb_populate_record(o, jsonb_build_object('id', @id::uuid, 'code', 'SN-COPY'))).* FROM orders o WHERE o.id = @order`,
		pgx.StrictNamedArgs{"id": uuid.NewV7(), "order": again.Order.ID})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23505" || pgErr.ConstraintName != "orders_open_per_deal" {
		t.Fatalf("a second open order on the deal: %v", err)
	}
}
