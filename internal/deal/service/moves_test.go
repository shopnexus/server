package service_test

import (
	"context"
	"slices"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/deal/domain"
)

// Moves reads the moves a notification names, with the people and the
// listing, and leaves out an ID the deal lacks.
func TestMoves(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	l := catalogtest.Listing(t, e.db, seller)
	v := e.open(t, buyer, l)
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	var offer uuid.UUID
	if err := e.db.QueryRow(ctx, `SELECT id FROM deal_moves WHERE deal_id = @id AND kind = 'offer'`,
		pgx.StrictNamedArgs{"id": v.ID}).Scan(&offer); err != nil {
		t.Fatal(err)
	}
	f, ok, err := e.deals.Moves(ctx, v.ID, []uuid.UUID{offer, uuid.NewV7()})
	if err != nil || !ok || f.BuyerID != buyer || f.SellerID != seller || f.ListingTitle == "" ||
		f.BuyerName == "" || len(f.Moves) != 1 || f.Moves[0].Kind != domain.MoveOffer || f.Moves[0].By != domain.Buyer ||
		f.Moves[0].Amount != 1_000_000 {
		t.Fatalf("%+v %v %v", f, ok, err)
	}
	if _, ok, err = e.deals.Moves(ctx, uuid.NewV7(), nil); err != nil || ok {
		t.Fatalf("an unknown deal: %v %v", ok, err)
	}
}

// Moves reads the order each step names: after the buyer asked again, at a
// price agreed since, the steps of the cancelled order read the cancelled
// order. The latest order comes apart too, for a move that names none.
func TestMovesNameTheirOrders(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	first := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	must(t)(e.deals.CancelOrder(ctx, buyer, first.Order.ID, ""))
	must(t)(e.deals.Offer(ctx, buyer, first.ID, 1_200_000))
	must(t)(e.deals.Accept(ctx, seller, first.ID, 1_200_000))
	second, err := e.deals.RequestCod(ctx, buyer, first.ID, 1_200_000, e.address(t, buyer), "")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.db.Query(ctx, `SELECT id FROM deal_moves WHERE deal_id = @id`, pgx.StrictNamedArgs{"id": first.ID})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := e.deals.Moves(ctx, first.ID, ids)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range f.Moves {
		var id uuid.UUID
		if m.OrderID != nil {
			id = *m.OrderID
		}
		switch id {
		case first.Order.ID:
			got = append(got, string(m.Kind)+" first")
		case second.Order.ID:
			got = append(got, string(m.Kind)+" second")
		case uuid.UUID{}:
			got = append(got, string(m.Kind))
		default:
			got = append(got, string(m.Kind)+" another order")
		}
	}
	want := []string{"cod_requested first", "order_cancelled first", "offer", "accept", "cod_requested second"}
	if !slices.Equal(got, want) {
		t.Fatalf("moves %v, want %v", got, want)
	}
	if len(f.Orders) != 2 || f.Orders[first.Order.ID].Status != domain.OrderCancelled ||
		f.Orders[first.Order.ID].CancelReason != domain.CancelByBuyer || f.Orders[second.Order.ID].Status != domain.OrderPending {
		t.Fatalf("orders %+v", f.Orders)
	}
	if f.Order == nil || f.Order.ID != second.Order.ID {
		t.Fatalf("latest order %+v", f.Order)
	}
	var offered uuid.UUID
	if err = e.db.QueryRow(ctx, `SELECT id FROM deal_moves WHERE deal_id = @id AND kind = 'offer'`,
		pgx.StrictNamedArgs{"id": first.ID}).Scan(&offered); err != nil {
		t.Fatal(err)
	}
	offer, _, err := e.deals.Moves(ctx, first.ID, []uuid.UUID{offered})
	if err != nil || offer.Order == nil || len(offer.Orders) != 1 || offer.Orders[second.Order.ID].ID != second.Order.ID {
		t.Fatalf("a move on the price reads the latest order alone: %+v %v", offer, err)
	}
}
