package repository_test

import (
	"context"
	"maps"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// A buyer's figures count the orders requested since the cut and the
// failed ones among them; open counts pending and shipped orders whenever
// they were requested.
func TestBuyerOrders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	buyer, seller := accounttest.New(t, db), accounttest.New(t, db)
	pendingDeal(t, db, s, buyer, seller)
	soldDeal(t, db, s, buyer, seller)
	failed := pendingDeal(t, db, s, buyer, seller)
	sent := domain.Shipment{Mode: domain.ModeManual, Carrier: domain.CarrierSPX, TrackingCode: "SPX2", Status: domain.ShipmentPendingPickup}
	if err := failed.Ship(domain.Seller, failed.Order().ID, sent, onSale, now); err != nil {
		t.Fatal(err)
	}
	if _, err := failed.ShipmentUpdated(failed.Order().ID, domain.ShipmentFailed, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &failed, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	old := pendingDeal(t, db, s, buyer, seller)
	if _, err := db.Exec(ctx, `UPDATE orders SET requested_at = @at WHERE id = @id`,
		pgx.StrictNamedArgs{"id": old.Order().ID, "at": now.Add(-100 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	got, err := s.BuyerOrders(ctx, buyer, now.Add(-domain.BuyerPeriod))
	if err != nil || got != (domain.BuyerOrders{Placed: 3, Failed: 1, Open: 2}) {
		t.Fatalf("buyer: %+v %v", got, err)
	}
	if got, _ = s.BuyerOrders(ctx, seller, now.Add(-domain.BuyerPeriod)); got != (domain.BuyerOrders{}) {
		t.Fatalf("the seller bought nothing: %+v", got)
	}
	all, err := s.CountOrders(ctx, time.Time{})
	if want := map[domain.OrderStatus]int{domain.OrderPending: 2, domain.OrderCompleted: 1, domain.OrderFailed: 1}; err != nil ||
		!maps.Equal(all, want) {
		t.Fatalf("every order: %v %v", all, err)
	}
	recent, err := s.CountOrders(ctx, now.Add(-domain.BuyerPeriod))
	if want := map[domain.OrderStatus]int{domain.OrderPending: 1, domain.OrderCompleted: 1, domain.OrderFailed: 1}; err != nil ||
		!maps.Equal(recent, want) {
		t.Fatalf("recent orders: %v %v", recent, err)
	}
}
