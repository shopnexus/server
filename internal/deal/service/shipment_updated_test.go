package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/provider/carrier"
)

// UC-DEAL-05 A1, E2 · NFR-IDEM-01.
func TestShipmentUpdated(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.shipped(t, buyer, seller, listing)
	// Waiting for pickup, the order is due at the pickup timeout.
	got := e.get(t, buyer, v.ID)
	if o := got.Order; o.Shipment.PickedUpAt != nil || o.DueAt == nil || !o.DueAt.Equal(o.ShippedAt.Add(domain.OrderWaits.Pickup)) {
		t.Fatalf("waiting for pickup: %+v %+v", o, o.Shipment)
	}

	if err := report(ctx, e.deals, v, "in_transit"); err != nil {
		t.Fatal(err)
	}
	got = e.get(t, buyer, v.ID)
	if got.Order.Shipment.Status != domain.ShipmentInTransit || got.Order.Status != domain.OrderShipped ||
		got.Order.Shipment.PickedUpAt == nil || got.Order.DueAt != nil {
		t.Fatalf("in transit: %+v %+v", got.Order, got.Order.Shipment)
	}
	if m := got.Moves[len(got.Moves)-1]; m.Kind != domain.MoveShipmentUpdated || m.By != domain.System {
		t.Fatalf("%+v", m)
	}
	if err := report(ctx, e.deals, v, "in_transit"); err != nil {
		t.Fatal(err)
	}
	if n := countMoves(moveKinds(e.get(t, buyer, v.ID)), domain.MoveShipmentUpdated); n != 1 {
		t.Fatalf("the same report twice made %d moves", n)
	}

	if err := report(ctx, e.deals, v, "failed"); err != nil {
		t.Fatal(err)
	}
	got = e.get(t, buyer, v.ID)
	if got.Order.Status != domain.OrderFailed || got.Order.ClosedAt == nil || got.Status != domain.StatusNegotiating ||
		got.AgreedPrice != nil || got.Moves[len(got.Moves)-1].Kind != domain.MoveFailed {
		t.Fatalf("failed: %+v", got)
	}
	if st, held := e.summary(t, listing); st != catalogdomain.StatusActive || held != (uuid.UUID{}) {
		t.Fatalf("listing %s for %s", st, held)
	}
	moves := len(got.Moves)
	if err := report(ctx, e.deals, v, "delivered"); err != nil {
		t.Fatalf("a late report: %v", err)
	}
	if got = e.get(t, buyer, v.ID); got.Order.Status != domain.OrderFailed || len(got.Moves) != moves {
		t.Fatalf("a late report moved the order: %+v", got.Order)
	}

	err := e.deals.ShipmentUpdated(ctx, "ghtk", "GHTK-NOBODY", "delivered")
	if !errors.Is(err, domain.ErrShipmentNotFound) || apperr.From(err).Kind != apperr.KindNotFound {
		t.Fatalf("an unknown code: %v", err)
	}
	if err = e.deals.ShipmentUpdated(ctx, "ghn", v.Order.Shipment.TrackingCode, "delivered"); !errors.Is(err, domain.ErrShipmentNotFound) {
		t.Fatalf("the code at another carrier: %v", err)
	}
	if err = report(ctx, e.deals, v, "returned"); !errors.Is(err, domain.ErrInvalidStatus) {
		t.Fatalf("an unknown status: %v", err)
	}
	// Mua lại: the buyer asks for COD again on the same deal.
	again, err := e.deals.RequestCod(ctx, buyer, v.ID, 1_500_000, e.address(t, buyer), uuid.Nil())
	if err != nil || again.Order.Status != domain.OrderPending || again.Order.ID == v.Order.ID {
		t.Fatalf("buy again: %+v %v", again.Order, err)
	}
}

// oneParcel is the fake GHTK making every shipment under one code, as a
// manual shipment of two items in one parcel did before 2026-11-25.
type oneParcel struct{ carrier.Fake }

func (oneParcel) Create(context.Context, carrier.Account, carrier.CreateInput) (carrier.Created, error) {
	return carrier.Created{TrackingCode: "GHTK-PARCEL1", Fee: 22_000}, nil
}

// UC-DEAL-05.
func TestOneParcelTwoOrders(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	first, second := catalogtest.Listing(t, e.db, seller), catalogtest.Listing(t, e.db, seller)
	deps := e.deps
	deps.Shipping = linkedAt{oneParcel{carrier.Fake{Carrier: carrier.GHTK}}}
	deals := start(t, deps)
	a, b := e.pending(t, buyer, first), e.pending(t, buyer, second)
	must(t)(deals.CreateShipment(ctx, seller, a.Order.ID, key(), linked))
	must(t)(deals.CreateShipment(ctx, seller, b.Order.ID, key(), linked))
	if err := e.deals.ShipmentUpdated(ctx, "ghtk", "GHTK-PARCEL1", "failed"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		deal    uuid.UUID
		listing uuid.UUID
	}{{a.ID, first}, {b.ID, second}} {
		if got := e.get(t, buyer, c.deal); got.Order.Status != domain.OrderFailed {
			t.Errorf("%s: %+v", c.deal, got.Order)
		}
		if st, _ := e.summary(t, c.listing); st != catalogdomain.StatusActive {
			t.Errorf("listing %s", st)
		}
	}
}

// confirmFirst is a store where the buyer confirms receipt just before the
// system saves a carrier's report, so the report's save conflicts.
type confirmFirst struct {
	repository.Deals

	fired *atomic.Bool
}

func (c confirmFirst) Save(ctx context.Context, d *domain.Deal, writeID, key uuid.UUID, evs ...events.Event) error {
	if o := d.Order(); o != nil && o.Status == domain.OrderShipped && o.Shipment.Status == domain.ShipmentInTransit &&
		c.fired.CompareAndSwap(false, true) {
		cur, err := c.Deals.Get(ctx, d.ID())
		if err != nil {
			return err
		}
		if err = cur.ConfirmReceipt(domain.Buyer, cur.Order().ID, time.Now()); err != nil {
			return err
		}
		if err = c.Deals.Save(ctx, &cur, uuid.NewV7(), uuid.Nil()); err != nil {
			return err
		}
	}
	return c.Deals.Save(ctx, d, writeID, key, evs...)
}

func TestShipmentUpdateLosesToAConfirm(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	v := e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller))
	deps := e.deps
	deps.Deals = confirmFirst{Deals: e.repo, fired: &atomic.Bool{}}
	if err := report(context.Background(), start(t, deps), v, "in_transit"); err != nil {
		t.Fatalf("the report: %v", err)
	}
	got := e.get(t, buyer, v.ID)
	if got.Order.Status != domain.OrderCompleted || got.Order.Shipment.Status != domain.ShipmentPendingPickup ||
		countMoves(moveKinds(got), domain.MoveShipmentUpdated) != 0 {
		t.Fatalf("%+v", got.Order)
	}
}
