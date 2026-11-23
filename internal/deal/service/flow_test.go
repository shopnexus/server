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
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/provider/carrier"
)

// manual is a shipment the seller made on SPX's own app, under a code the
// fake carrier knows.
func manual(code string) service.ShipmentInput {
	return service.ShipmentInput{Mode: "manual", Carrier: "spx", TrackingCode: code}
}

// linked is a shipment to make on the seller's GHTK account.
var linked = service.ShipmentInput{Mode: "linked", Carrier: "ghtk", Service: "standard", WeightGrams: 800, Pickup: "home"}

func key() string { return uuid.NewV7().String() }

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
// under code with the fake carrier.
func (e env) shipped(t *testing.T, buyer, seller, listing uuid.UUID, code string) service.DealView {
	t.Helper()
	v := e.pending(t, buyer, listing)
	s, err := e.deals.CreateShipment(context.Background(), seller, v.Order.ID, key(), manual(code))
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
		s, err := e.deals.CreateShipment(ctx, seller, v.Order.ID, k, manual(" SPXVN0123 "))
		if err != nil || s.Order.Status != domain.OrderShipped || s.Order.Shipment == nil || s.Order.ShippedAt == nil {
			t.Fatalf("ship: %+v %v", s.Order, err)
		}
		sh := s.Order.Shipment
		if sh.Mode != domain.ModeManual || sh.Carrier != domain.CarrierSPX || sh.TrackingCode != "SPXVN0123" ||
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
	v := e.shipped(t, buyer, seller, listing, "SPXVN0123")
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

// UC-DEAL-04 · ADR-0001.
func TestLinkedShipment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	s, err := e.deals.CreateShipment(ctx, seller, v.Order.ID, key(), linked)
	if err != nil || s.Order.Status != domain.OrderShipped {
		t.Fatalf("%+v %v", s.Order, err)
	}
	if sh := s.Order.Shipment; sh.Mode != domain.ModeLinked || sh.Carrier != domain.CarrierGHTK ||
		!strings.HasPrefix(sh.TrackingCode, "GHTK-") || sh.Fee == nil || *sh.Fee != 22_000 || sh.Status != domain.ShipmentPendingPickup {
		t.Fatalf("%+v", sh)
	}
	if _, err = e.deals.EditShipment(ctx, seller, v.Order.ID, "spx", "SPX1"); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("edit a linked shipment: %v", err)
	}
}

// UC-DEAL-04 A3.
func TestEditShipment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	v := e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller), "SPXVN0123")
	got, err := e.deals.EditShipment(ctx, seller, v.Order.ID, "ghn", " GHN0042 ")
	if err != nil {
		t.Fatal(err)
	}
	if sh := got.Order.Shipment; sh.Mode != domain.ModeManual || sh.Carrier != domain.CarrierGHN || sh.TrackingCode != "GHN0042" ||
		sh.Status != domain.ShipmentPendingPickup || got.Order.Status != domain.OrderShipped {
		t.Fatalf("%+v", got.Order)
	}
	if m := got.Moves[len(got.Moves)-1]; m.Kind != domain.MoveShipmentUpdated || m.By != domain.Seller {
		t.Fatalf("%+v", m)
	}
	if _, err = e.deals.EditShipment(ctx, buyer, v.Order.ID, "ghn", "GHN1"); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("the buyer edits: %v", err)
	}
	if _, err = e.deals.EditShipment(ctx, stranger, v.Order.ID, "ghn", "GHN1"); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Errorf("a stranger edits: %v", err)
	}
	if _, err = e.deals.EditShipment(ctx, seller, v.Order.ID, "ghn", "NOTFOUND1"); !errors.Is(err, domain.ErrShipmentUnknown) ||
		apperr.From(err).Kind != apperr.KindInvalid {
		t.Errorf("a code the carrier does not know: %v", err)
	}
	if _, err = e.deals.EditShipment(ctx, seller, v.Order.ID, "other", "X1"); !errors.Is(err, domain.ErrInvalidShipment) {
		t.Errorf("carrier other: %v", err)
	}
	if got = e.get(t, buyer, v.ID); got.Order.Shipment.TrackingCode != "GHN0042" {
		t.Fatalf("a refused edit changed the shipment: %+v", got.Order.Shipment)
	}
}

// UC-DEAL-04.
func TestLookupShipment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	p, err := e.deals.LookupShipment(ctx, uuid.NewV7(), "spx", " SPX1 ")
	if err != nil || p.Carrier != domain.CarrierSPX || p.TrackingCode != "SPX1" || p.CODAmount != 1_500_000 ||
		p.Status != domain.ShipmentPendingPickup || p.Fee == nil || p.RecipientName == "" || p.District == "" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err = e.deals.LookupShipment(ctx, uuid.NewV7(), "spx", "NOTFOUND"); !errors.Is(err, domain.ErrShipmentNotFound) ||
		apperr.From(err).Kind != apperr.KindNotFound {
		t.Errorf("an unknown code: %v", err)
	}
	if _, err = e.deals.LookupShipment(ctx, uuid.NewV7(), "other", "X1"); !errors.Is(err, domain.ErrInvalidShipment) {
		t.Errorf("carrier other: %v", err)
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
	for _, in := range []service.ShipmentInput{manual("SPX1"), linked} {
		if _, err := e.deals.CreateShipment(ctx, seller, v.Order.ID, key(), in); !errors.Is(err, domain.ErrListingUnavailable) {
			t.Fatalf("%s: %v", in.Mode, err)
		}
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

func (c cancelFirst) Save(ctx context.Context, d *domain.Deal, writeID uuid.UUID, evs ...events.Event) error {
	if o := d.Order(); o != nil && o.Status == domain.OrderShipped && c.fired.CompareAndSwap(false, true) {
		cur, err := c.Deals.Get(ctx, d.ID())
		if err != nil {
			return err
		}
		if err = cur.Cancel(domain.Buyer, time.Now()); err != nil {
			return err
		}
		if err = c.Deals.Save(ctx, &cur, uuid.NewV7()); err != nil {
			return err
		}
	}
	return c.Deals.Save(ctx, d, writeID, evs...)
}

// UC-DEAL-04 E7 · NFR-CONC-01.
func TestShipLosesToACancel(t *testing.T) {
	t.Parallel()
	for _, in := range []service.ShipmentInput{manual("SPX1"), linked} {
		e := newEnv(t)
		ctx := context.Background()
		seller, buyer := e.person(t), e.person(t)
		listing := catalogtest.Listing(t, e.db, seller)
		v := e.pending(t, buyer, listing)
		cancels := &atomic.Int32{}
		deps := e.deps
		deps.Deals = cancelFirst{Deals: e.repo, fired: &atomic.Bool{}}
		deps.Shipping = linkedAt{countingCarrier{Carrier: carrier.GHTK, cancels: cancels}}
		if _, err := start(t, deps).CreateShipment(ctx, seller, v.Order.ID, key(), in); !errors.Is(err, domain.ErrChanged) {
			t.Fatalf("%s: %v", in.Mode, err)
		}
		if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderCancelled || got.Order.CancelReason != domain.CancelByBuyer {
			t.Fatalf("%s: %+v", in.Mode, got.Order)
		}
		if st, _ := e.summary(t, listing); st != catalogdomain.StatusActive {
			t.Fatalf("%s: listing %s", in.Mode, st)
		}
		if k := pgtest.EventKinds(t, e.db, listing); count(k, "listing.reserved") != 1 || k[len(k)-1] != "listing.released" {
			t.Fatalf("%s: events %v", in.Mode, k)
		}
		// The GHTK shipment the order did not take is called off; a manual
		// one is the seller's own.
		if want := map[string]int32{"manual": 0, "linked": 1}[in.Mode]; cancels.Load() != want {
			t.Fatalf("%s: %d cancels at the carrier", in.Mode, cancels.Load())
		}
	}
}

// UC-DEAL-04 E1, E6, E8 · UC-DEAL-05 E3.
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
		"the buyer ships":          {buyer, manual("SPX1"), domain.ErrMoveNotAllowed},
		"a stranger ships":         {stranger, manual("SPX1"), domain.ErrOrderNotFound},
		"carrier other":            {seller, service.ShipmentInput{Mode: "manual", Carrier: "other", TrackingCode: "X1"}, domain.ErrInvalidShipment},
		"a code of spaces":         {seller, manual("   "), domain.ErrInvalidShipment},
		"a code the carrier lacks": {seller, manual("NOTFOUND-7"), domain.ErrShipmentUnknown},
		"a parcel of 0 grams":      {seller, service.ShipmentInput{Mode: "linked", Service: "standard", Pickup: "home"}, domain.ErrInvalidShipment},
		"a pickup at the door":     {seller, service.ShipmentInput{Mode: "linked", Service: "standard", WeightGrams: 800, Pickup: "door"}, domain.ErrInvalidShipment},
	} {
		if _, err := e.deals.CreateShipment(ctx, c.caller, v.Order.ID, key(), c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.deals.ConfirmReceipt(ctx, buyer, v.Order.ID, key()); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("confirm a pending order: %v", err)
	}
	must(t)(e.deals.CreateShipment(ctx, seller, v.Order.ID, key(), manual("SPX1")))
	if _, err := e.deals.ConfirmReceipt(ctx, seller, v.Order.ID, key()); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("the seller confirms: %v", err)
	}
	if _, err := e.deals.ConfirmReceipt(ctx, stranger, v.Order.ID, key()); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Errorf("a stranger confirms: %v", err)
	}
	if _, err := e.deals.CreateShipment(ctx, seller, v.Order.ID, key(), manual("SPX2")); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("ship a shipped order: %v", err)
	}
}

// shipFirst is a store where another try ships the order just before this
// one's save, as a second tap with a key of its own would.
type shipFirst struct {
	repository.Deals

	fired *atomic.Bool
}

func (c shipFirst) Save(ctx context.Context, d *domain.Deal, writeID uuid.UUID, evs ...events.Event) error {
	if o := d.Order(); o != nil && o.Status == domain.OrderShipped && c.fired.CompareAndSwap(false, true) {
		cur, err := c.Deals.Get(ctx, d.ID())
		if err != nil {
			return err
		}
		listing := domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingActive}
		if err = cur.Ship(domain.Seller, cur.Order().ID, *o.Shipment, listing, time.Now()); err != nil {
			return err
		}
		if err = c.Deals.Save(ctx, &cur, uuid.NewV7()); err != nil {
			return err
		}
	}
	return c.Deals.Save(ctx, d, writeID, evs...)
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
	s, err := start(t, deps).CreateShipment(ctx, seller, v.Order.ID, key(), manual("SPX1"))
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
	first := e.shipped(t, buyer, seller, listing, "SPXFIRST1")
	if err := e.deals.ShipmentUpdated(ctx, "SPXFIRST1", "failed"); err != nil {
		t.Fatal(err)
	}
	must(t)(e.deals.Accept(ctx, buyer, first.ID, 1_500_000))
	second, err := e.deals.RequestCod(ctx, buyer, first.ID, e.address(t, buyer), "")
	if err != nil || second.Order.ID == first.Order.ID {
		t.Fatalf("second order: %+v %v", second.Order, err)
	}
	must(t)(e.deals.CreateShipment(ctx, seller, second.Order.ID, key(), manual("SPXSECOND1")))
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
