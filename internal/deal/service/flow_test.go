package service_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/provider/carrier"
)

// linked is a shipment to make on the seller's GHTK account.
var linked = service.ShipmentInput{Carrier: "ghtk", Service: "standard", WeightGrams: 800, Pickup: "home"}

// report moves v's shipment to status as its carrier would report it, of
// a step that happened now.
func report(ctx context.Context, d *service.Deals, v service.DealView, status string) error {
	return reportAt(ctx, d, v, status, time.Time{})
}

// reportAt is report of a step that happened at.
func reportAt(ctx context.Context, d *service.Deals, v service.DealView, status string, at time.Time) error {
	sh := v.Order.Shipment
	return d.ShipmentUpdated(ctx, string(sh.Carrier), sh.TrackingCode, status, at)
}

func key() uuid.UUID { return uuid.NewV7() }

// summary reads listing through catalog.
func (e env) summary(t *testing.T, listing uuid.UUID) (catalogdomain.Status, uuid.UUID) {
	t.Helper()
	l, _, err := e.catalog.Summary(context.Background(), listing)
	if err != nil {
		t.Fatal(err)
	}
	return l.Status, l.ReservedFor
}

func count(kinds []string, kind string) int {
	n := 0
	for _, k := range kinds {
		if k == kind {
			n++
		}
	}
	return n
}

// shipped is a pending order of buyer's on listing, shipped by its seller
// on the fake GHTK.
func (e env) shipped(t *testing.T, buyer, seller, listing uuid.UUID) service.DealView {
	t.Helper()
	v := e.pending(t, buyer, listing)
	s, err := e.deals.CreateShipment(context.Background(), seller, v.Order.ID, key(), linked)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// countingCarrier is the fake GHTK, counting the shipments it calls off.
type countingCarrier struct {
	carrier.Fake

	cancels *atomic.Int32
}

func (c countingCarrier) Cancel(ctx context.Context, a carrier.Account, code string) error {
	c.cancels.Add(1)
	return c.Fake.Cancel(ctx, a, code)
}

// UC-DEAL-04 · UC-DEAL-05 A2.
func TestShipThenConfirm(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	k := key()
	for range 2 {
		s, err := e.deals.CreateShipment(ctx, seller, v.Order.ID, k, linked)
		if err != nil || s.Order.Status != domain.OrderShipped || s.Order.Shipment == nil || s.Order.ShippedAt == nil {
			t.Fatalf("ship: %+v %v", s.Order, err)
		}
		sh := s.Order.Shipment
		if sh.Mode != domain.ModeLinked || sh.Carrier != domain.CarrierGHTK || !strings.HasPrefix(sh.TrackingCode, "GHTK-") ||
			sh.Fee == nil || *sh.Fee != 22_000 || sh.Status != domain.ShipmentPendingPickup || sh.DeliveredAt != nil {
			t.Fatalf("shipment: %+v", sh)
		}
	}
	if st, held := e.summary(t, listing); st != catalogdomain.StatusReserved || held != v.Order.ID {
		t.Fatalf("listing %s for %s", st, held)
	}
	if n := count(pgtest.EventKinds(t, e.db, listing), "listing.reserved"); n != 1 {
		t.Fatalf("listing.reserved %d times", n)
	}
	if n := countMoves(moveKinds(e.get(t, buyer, v.ID)), domain.MoveShipped); n != 1 {
		t.Fatalf("shipped %d times", n)
	}
	c, err := e.deals.ConfirmReceipt(ctx, buyer, v.Order.ID, key())
	if err != nil || c.Status != domain.StatusSold || c.Order.Status != domain.OrderCompleted || c.Order.ClosedAt == nil {
		t.Fatalf("confirm: %+v %v", c, err)
	}
	if m := c.Moves[len(c.Moves)-1]; m.Kind != domain.MoveCompleted || m.By != domain.Buyer {
		t.Fatalf("%+v", m)
	}
	if st, _ := e.summary(t, listing); st != catalogdomain.StatusSold {
		t.Fatalf("listing %s", st)
	}
}

// UC-DEAL-05 A2 · NFR-IDEM-01.
func TestConfirmTwice(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.shipped(t, buyer, seller, listing)
	k := key()
	for range 2 {
		c, err := e.deals.ConfirmReceipt(ctx, buyer, v.Order.ID, k)
		if err != nil || c.Order.Status != domain.OrderCompleted {
			t.Fatalf("confirm with one key: %+v %v", c.Order, err)
		}
	}
	if _, err := e.deals.ConfirmReceipt(ctx, buyer, v.Order.ID, key()); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("confirm a completed order: %v", err)
	}
	if n := count(pgtest.EventKinds(t, e.db, listing), "listing.sold"); n != 1 {
		t.Fatalf("listing.sold %d times", n)
	}
	if n := countMoves(moveKinds(e.get(t, seller, v.ID)), domain.MoveCompleted); n != 1 {
		t.Fatalf("completed %d times", n)
	}
}

// UC-DEAL-04 E5.
func TestShipRefusesAHiddenListing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	if _, err := e.catalog.Hide(ctx, seller, listing, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	if _, err := e.deals.CreateShipment(ctx, seller, v.Order.ID, key(), linked); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Fatal(err)
	}
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderPending {
		t.Fatalf("%+v", got.Order)
	}
}

// cancelFirst is a store where the buyer cancels just before the seller's
// shipment is saved, so the shipment's save conflicts.
type cancelFirst struct {
	repository.Deals

	fired *atomic.Bool
}

func (c cancelFirst) Save(ctx context.Context, d *domain.Deal, writeID, key uuid.UUID, evs ...events.Event) error {
	if o := d.Order(); o != nil && o.Status == domain.OrderShipped && c.fired.CompareAndSwap(false, true) {
		cur, err := c.Deals.Get(ctx, d.ID())
		if err != nil {
			return err
		}
		if err = cur.Cancel(domain.Buyer, time.Now()); err != nil {
			return err
		}
		if err = c.Deals.Save(ctx, &cur, uuid.NewV7(), uuid.Nil()); err != nil {
			return err
		}
	}
	return c.Deals.Save(ctx, d, writeID, key, evs...)
}

// UC-DEAL-04 E7 · NFR-CONC-01.
func TestShipLosesToACancel(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	cancels := &atomic.Int32{}
	deps := e.deps
	deps.Deals = cancelFirst{Deals: e.repo, fired: &atomic.Bool{}}
	deps.Shipping = linkedAt{countingCarrier{Carrier: carrier.GHTK, cancels: cancels}}
	if _, err := start(t, deps).CreateShipment(ctx, seller, v.Order.ID, key(), linked); !errors.Is(err, domain.ErrChanged) {
		t.Fatal(err)
	}
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderCancelled || got.Order.CancelReason != domain.CancelByBuyer {
		t.Fatalf("%+v", got.Order)
	}
	if st, _ := e.summary(t, listing); st != catalogdomain.StatusActive {
		t.Fatalf("listing %s", st)
	}
	if k := pgtest.EventKinds(t, e.db, listing); count(k, "listing.reserved") != 1 || k[len(k)-1] != "listing.released" {
		t.Fatalf("events %v", k)
	}
	// The GHTK shipment the order did not take is called off.
	if cancels.Load() != 1 {
		t.Fatalf("%d cancels at the carrier", cancels.Load())
	}
}

// UC-DEAL-04 E6, E8 · UC-DEAL-05 E3.
func TestShipRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	for name, c := range map[string]struct {
		caller uuid.UUID
		in     service.ShipmentInput
		want   error
	}{
		"the buyer ships":      {buyer, linked, domain.ErrMoveNotAllowed},
		"a stranger ships":     {stranger, linked, domain.ErrOrderNotFound},
		"carrier spx":          {seller, service.ShipmentInput{Carrier: "spx", Service: "standard", WeightGrams: 800, Pickup: "home"}, domain.ErrInvalidShipment},
		"a parcel of 0 grams":  {seller, service.ShipmentInput{Carrier: "ghtk", Service: "standard", Pickup: "home"}, domain.ErrInvalidShipment},
		"a pickup at the door": {seller, service.ShipmentInput{Carrier: "ghtk", Service: "standard", WeightGrams: 800, Pickup: "door"}, domain.ErrInvalidShipment},
	} {
		if _, err := e.deals.CreateShipment(ctx, c.caller, v.Order.ID, key(), c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.deals.ConfirmReceipt(ctx, buyer, v.Order.ID, key()); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("confirm a pending order: %v", err)
	}
	must(t)(e.deals.CreateShipment(ctx, seller, v.Order.ID, key(), linked))
	if _, err := e.deals.ConfirmReceipt(ctx, seller, v.Order.ID, key()); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("the seller confirms: %v", err)
	}
	if _, err := e.deals.ConfirmReceipt(ctx, stranger, v.Order.ID, key()); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Errorf("a stranger confirms: %v", err)
	}
	if _, err := e.deals.CreateShipment(ctx, seller, v.Order.ID, key(), linked); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("ship a shipped order: %v", err)
	}
}

// shipFirst is a store where another try ships the order just before this
// one's save, as a second tap with a key of its own would.
type shipFirst struct {
	repository.Deals

	fired *atomic.Bool
}

func (c shipFirst) Save(ctx context.Context, d *domain.Deal, writeID, key uuid.UUID, evs ...events.Event) error {
	if o := d.Order(); o != nil && o.Status == domain.OrderShipped && c.fired.CompareAndSwap(false, true) {
		cur, err := c.Deals.Get(ctx, d.ID())
		if err != nil {
			return err
		}
		listing := domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingActive}
		if err = cur.Ship(domain.Seller, cur.Order().ID, *o.Shipment, listing, time.Now()); err != nil {
			return err
		}
		if err = c.Deals.Save(ctx, &cur, uuid.NewV7(), uuid.Nil()); err != nil {
			return err
		}
	}
	return c.Deals.Save(ctx, d, writeID, key, evs...)
}

// UC-DEAL-04 · NFR-CONC-01.
func TestShipTwiceKeepsTheListing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	deps := e.deps
	deps.Deals = shipFirst{Deals: e.repo, fired: &atomic.Bool{}}
	s, err := start(t, deps).CreateShipment(ctx, seller, v.Order.ID, key(), linked)
	if err != nil || s.Order.Status != domain.OrderShipped {
		t.Fatalf("ship: %+v %v", s.Order, err)
	}
	if st, held := e.summary(t, listing); st != catalogdomain.StatusReserved || held != v.Order.ID {
		t.Fatalf("listing %s for %s", st, held)
	}
}

// UC-DEAL-05 E3.
func TestConfirmRefusesAFailedOrder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	first := e.shipped(t, buyer, seller, listing)
	if err := report(ctx, e.deals, first, "failed"); err != nil {
		t.Fatal(err)
	}
	second, err := e.deals.RequestCod(ctx, buyer, first.ID, 1_500_000, e.address(t, buyer), uuid.Nil())
	if err != nil || second.Order.ID == first.Order.ID {
		t.Fatalf("second order: %+v %v", second.Order, err)
	}
	must(t)(e.deals.CreateShipment(ctx, seller, second.Order.ID, key(), linked))
	if _, err = e.deals.ConfirmReceipt(ctx, buyer, first.Order.ID, key()); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("confirm the failed order: %v", err)
	}
	if v := e.get(t, buyer, first.ID); v.Order.ID != second.Order.ID || v.Order.Status != domain.OrderShipped || v.Status != domain.StatusAgreed {
		t.Fatalf("the second order moved: %+v %s", v.Order, v.Status)
	}
	if st, held := e.summary(t, listing); st != catalogdomain.StatusReserved || held != second.Order.ID {
		t.Fatalf("listing %s for %s", st, held)
	}
}
