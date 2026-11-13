package repository_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// shipLinked ships d's order as a linked shipment at c picked up from.
func shipLinked(t *testing.T, s repository.Deals, d domain.Deal, c domain.Carrier, from *domain.ShipmentPickup) domain.Deal {
	t.Helper()
	ctx := context.Background()
	got, err := s.Get(ctx, d.ID())
	if err != nil {
		t.Fatal(err)
	}
	sent := domain.Shipment{Mode: domain.ModeLinked, Carrier: c, TrackingCode: "L-" + uuid.NewV7().String()[:8], Status: domain.ShipmentPendingPickup, Pickup: from}
	if err = got.Ship(domain.Seller, got.Order().ID, sent, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestALinkedShipmentKeepsItsPickup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	copied := &domain.ShipmentPickup{Name: "Khánh Linh", Phone: "+84912345812", Street: "12 Nguyễn Gia Trí", CommuneCode: "00004"}
	for _, from := range []*domain.ShipmentPickup{copied, {PointID: "88256"}, nil} {
		d := shipLinked(t, s, pendingDeal(t, db, s, buyer, seller), domain.CarrierGHN, from)
		got, err := s.Get(ctx, d.ID())
		sh := got.Order().Shipment
		if err != nil || (from == nil) != (sh.Pickup == nil) || (from != nil && *sh.Pickup != *from) {
			t.Fatalf("%+v: %+v %v", from, sh.Pickup, err)
		}
	}
	// A deleted seller's address leaves the orders; the point stays.
	if err := s.ScrubRecipients(ctx, seller); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListOrders(ctx, seller, domain.Seller, page.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if p := r.Order.Shipment.Pickup; p != nil && (p.Name != "" || p.Phone != "" || p.Street != "") {
			t.Errorf("after scrub: %+v", p)
		}
	}
}

func TestOpenShipmentsAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewDeals(db)
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	pendingDeal(t, db, s, buyer, seller)
	shipLinked(t, s, pendingDeal(t, db, s, buyer, seller), domain.CarrierGHN, nil)
	for c, want := range map[string]bool{"ghn": true, "ghtk": false} {
		if open, err := s.OpenShipmentsAt(ctx, seller, c); err != nil || open != want {
			t.Errorf("%s: %v %v", c, open, err)
		}
	}
	if open, _ := s.OpenShipmentsAt(ctx, buyer, "ghn"); open {
		t.Error("the buyer's purchase counts as the buyer's shipment")
	}
}
