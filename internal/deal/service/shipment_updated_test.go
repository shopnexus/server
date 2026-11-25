package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

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

	err := e.deals.ShipmentUpdated(ctx, "ghtk", "GHTK-NOBODY", "delivered", time.Time{}, nil)
	if !errors.Is(err, domain.ErrShipmentNotFound) || apperr.From(err).Kind != apperr.KindNotFound {
		t.Fatalf("an unknown code: %v", err)
	}
	if err = e.deals.ShipmentUpdated(ctx, "ghn", v.Order.Shipment.TrackingCode, "delivered", time.Time{}, nil); !errors.Is(err, domain.ErrShipmentNotFound) {
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
	return carrier.Created{TrackingCode: "GHTK-PARCEL1", Fee: new(int64(22_000))}, nil
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
	if err := e.deals.ShipmentUpdated(ctx, "ghtk", "GHTK-PARCEL1", "failed", time.Time{}, nil); err != nil {
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

// A report older than the order's last one lands nowhere: a late
// pending_pickup leaves a parcel picked up, so the pickup timeout cannot
// cancel it.
//
// UC-DEAL-05 A1.
func TestAnOlderReportChangesNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	v := e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller))
	at := time.Now()
	if err := reportAt(ctx, e.deals, v, "in_transit", at); err != nil {
		t.Fatal(err)
	}
	moves := len(e.get(t, buyer, v.ID).Moves)
	for _, old := range []time.Time{at, at.Add(-time.Minute)} {
		if err := reportAt(ctx, e.deals, v, "pending_pickup", old); err != nil {
			t.Fatal(err)
		}
	}
	got := e.get(t, buyer, v.ID)
	if sh := got.Order.Shipment; sh.Status != domain.ShipmentInTransit || sh.PickedUpAt == nil || len(got.Moves) != moves {
		t.Fatalf("an older report: %+v", sh)
	}
	if err := reportAt(ctx, e.deals, v, "delivered", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if sh := e.get(t, buyer, v.ID).Order.Shipment; sh.Status != domain.ShipmentDelivered {
		t.Fatalf("a newer report: %+v", sh)
	}
}

// A parcel the carrier lost after the pickup fails the order and puts the
// listing back on sale; one lost before it stays for the pickup timeout.
//
// UC-DEAL-05 E2.
func TestALostParcelFailsAnOrderPickedUp(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.shipped(t, buyer, seller, listing)
	if err := report(ctx, e.deals, v, "lost"); err != nil {
		t.Fatal(err)
	}
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderShipped || got.Order.Shipment.Status != domain.ShipmentPendingPickup {
		t.Fatalf("lost before the pickup: %+v", got.Order)
	}
	if err := report(ctx, e.deals, v, "in_transit"); err != nil {
		t.Fatal(err)
	}
	if err := report(ctx, e.deals, v, "lost"); err != nil {
		t.Fatal(err)
	}
	got := e.get(t, buyer, v.ID)
	if got.Order.Status != domain.OrderFailed || got.Order.Shipment.Status != domain.ShipmentFailed || got.Status != domain.StatusNegotiating ||
		got.Moves[len(got.Moves)-1].Kind != domain.MoveFailed {
		t.Fatalf("lost after the pickup: %+v", got)
	}
	if st, _ := e.summary(t, listing); st != catalogdomain.StatusActive {
		t.Fatalf("listing %s", st)
	}
}

// atShop is the fake carrier making each shipment at shop S1, as module
// shipping does with the seller's linked account.
type atShop struct{ linkedAt }

func (a atShop) Create(ctx context.Context, seller uuid.UUID, c carrier.Carrier, in carrier.CreateInput) (carrier.Created, error) {
	made, err := a.linkedAt.Create(ctx, seller, c, in)
	made.ShopCode = "S1"
	return made, err
}

// A shipment whose seller unlinked the carrier, or linked another shop,
// while it was made does not ship the order: the listing goes back, and
// the shipment, which the seller's account no longer reaches, is left to
// the seller at that shop.
//
// UC-DEAL-04 E7.
func TestAShipmentAtAShopNoLongerLinkedIsRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	cancels := &atomic.Int32{}
	deps := e.deps
	deps.Shipping = atShop{linkedAt{countingCarrier{Carrier: carrier.GHTK, cancels: cancels}}}
	deals := start(t, deps)
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	if _, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), linked); !errors.Is(err, domain.ErrCarrierNotLinked) {
		t.Fatalf("no account at the shop: %v", err)
	}
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderPending {
		t.Fatalf("%+v", got.Order)
	}
	if st, _ := e.summary(t, listing); st != catalogdomain.StatusActive || cancels.Load() != 0 {
		t.Fatalf("listing %s, %d cancels", st, cancels.Load())
	}
	if _, err := e.db.Exec(ctx, `INSERT INTO carrier_accounts (account_id, carrier, shop_code, token, linked_at)
VALUES (@seller, 'ghtk', 'S1', '\x01', now())`, pgx.StrictNamedArgs{"seller": seller}); err != nil {
		t.Fatal(err)
	}
	s, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), linked)
	if err != nil || s.Order.Status != domain.OrderShipped {
		t.Fatalf("at the linked shop: %+v %v", s.Order, err)
	}
}

// noFee is the fake GHTK answering a create without a fee, as GHTK
// answers a retry of a create that landed.
type noFee struct{ carrier.Fake }

func (f noFee) Create(ctx context.Context, a carrier.Account, in carrier.CreateInput) (carrier.Created, error) {
	c, err := f.Fake.Create(ctx, a, in)
	c.Fee = nil
	return c, err
}

// A shipment made without a fee stores none; a report's fee replaces the
// order's, and a report older than the order's step lands none.
//
// UC-DEAL-05 A1.
func TestShipmentFee(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	deps := e.deps
	deps.Shipping = linkedAt{noFee{carrier.Fake{Carrier: carrier.GHTK}}}
	deals := start(t, deps)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	v, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), linked)
	if err != nil {
		t.Fatal(err)
	}
	var stored *int64
	if err = e.db.QueryRow(ctx, `SELECT fee FROM orders WHERE id = @id`, pgx.StrictNamedArgs{"id": v.Order.ID}).Scan(&stored); err != nil || stored != nil {
		t.Fatalf("a shipment without a fee stored %v %v", stored, err)
	}
	sh := v.Order.Shipment
	t1 := time.Now().Add(-time.Hour).UTC()
	for _, r := range []struct {
		at   time.Time
		fee  int64
		want int64
	}{
		{t1, 30_400, 30_400},
		{t1, 31_000, 31_000},                   // a fee alone, at the stored step
		{t1.Add(-time.Minute), 16_500, 31_000}, // an older report
	} {
		if err = e.deals.ShipmentUpdated(ctx, string(sh.Carrier), sh.TrackingCode, "pending_pickup", r.at, new(r.fee)); err != nil {
			t.Fatal(err)
		}
		if got := e.get(t, buyer, v.ID).Order.Shipment.Fee; got == nil || *got != r.want {
			t.Fatalf("fee %d at %s: got %v", r.fee, r.at, got)
		}
	}
	if n := countMoves(moveKinds(e.get(t, buyer, v.ID)), domain.MoveShipmentUpdated); n != 0 {
		t.Fatalf("a fee made %d moves", n)
	}
}
