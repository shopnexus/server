package repository_test

import (
	"context"
	"slices"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// soldDeal stores a deal on a new listing of seller's whose order was
// completed.
func soldDeal(t *testing.T, db *pgxpool.Pool, s repository.Deals, buyer, seller uuid.UUID) domain.Deal {
	t.Helper()
	d := pendingDeal(t, db, s, buyer, seller)
	sent := domain.Shipment{Mode: domain.ModeManual, Carrier: domain.CarrierSPX, TrackingCode: "SPX1", Status: domain.ShipmentPendingPickup}
	if err := d.Ship(domain.Seller, d.Order().ID, sent, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err := d.ConfirmReceipt(domain.Buyer, d.Order().ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(context.Background(), &d, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOfPairs(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	ctx := context.Background()
	viewer, a, b, c, stranger := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	idle := inserted(t, db, s, catalogtest.Listing(t, db, viewer), a, viewer)
	soldDeal(t, db, s, viewer, b)
	soldDeal(t, db, s, a, viewer)
	open := inserted(t, db, s, catalogtest.Listing(t, db, a), viewer, a)
	soldDeal(t, db, s, viewer, a)
	soldB := soldDeal(t, db, s, viewer, b)
	inserted(t, db, s, catalogtest.Listing(t, db, viewer), stranger, viewer)
	if err := open.Offer(domain.Buyer, 1_000_000, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &open, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}

	got, err := s.OfPairs(ctx, viewer, []uuid.UUID{a, b, c})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 0, len(got))
	for i := range got {
		ids = append(ids, got[i].ID())
	}
	// With a, both active deals, the one without moves at its creation, and
	// no sold one; with b, only the newer sold deal.
	if want := []uuid.UUID{open.ID(), soldB.ID(), idle.ID()}; !slices.Equal(ids, want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	if o := got[1].Order(); o == nil || o.Status != domain.OrderCompleted {
		t.Fatalf("the sold deal's order: %+v", o)
	}
	if got, err = s.OfPairs(ctx, viewer, nil); err != nil || len(got) != 0 {
		t.Fatalf("no pairs: %d %v", len(got), err)
	}
}
