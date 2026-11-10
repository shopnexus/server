package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var spx = service.WaybillInput{Carrier: "spx", TrackingCode: "SPXVN0123", Fee: 22_000}

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

func TestShipDeliver(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.requested(t, buyer, listing)
	k := key()
	for range 2 {
		s, err := e.deals.Ship(ctx, seller, v.Order.ID, k, spx)
		if err != nil || s.Order.Status != domain.OrderShipped || s.Order.Waybill == nil || s.Order.Waybill.Fee != 22_000 {
			t.Fatalf("ship: %+v %v", s.Order, err)
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
	d, err := e.deals.Deliver(ctx, seller, v.Order.ID, key())
	if err != nil || d.Status != domain.StatusSold || d.Order.Status != domain.OrderDelivered {
		t.Fatalf("deliver: %+v %v", d, err)
	}
	if st, _ := e.summary(t, listing); st != catalogdomain.StatusSold {
		t.Fatalf("listing %s", st)
	}
}

func TestShipReturn(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.requested(t, buyer, listing)
	must(t)(e.deals.Ship(ctx, seller, v.Order.ID, key(), spx))
	r, err := e.deals.Return(ctx, seller, v.Order.ID, key())
	if err != nil || r.Status != domain.StatusNegotiating || r.Order.Status != domain.OrderReturned {
		t.Fatalf("return: %+v %v", r, err)
	}
	if st, held := e.summary(t, listing); st != catalogdomain.StatusActive || held != (uuid.UUID{}) {
		t.Fatalf("listing %s for %s", st, held)
	}
}

func TestFinishRefusesAnEarlierOrder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	first := e.requested(t, buyer, listing)
	must(t)(e.deals.Ship(ctx, seller, first.Order.ID, key(), spx))
	must(t)(e.deals.Return(ctx, seller, first.Order.ID, key()))
	second := e.requested(t, buyer, listing)
	if second.ID != first.ID || second.Order.ID == first.Order.ID {
		t.Fatalf("second order: %+v", second)
	}
	if _, err := e.deals.Ship(ctx, seller, first.Order.ID, key(), spx); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("ship the returned order: %v", err)
	}
	must(t)(e.deals.Ship(ctx, seller, second.Order.ID, key(), spx))
	for name, call := range map[string]func() error{
		"deliver": func() error { _, err := e.deals.Deliver(ctx, seller, first.Order.ID, key()); return err },
		"return":  func() error { _, err := e.deals.Return(ctx, seller, first.Order.ID, key()); return err },
	} {
		if err := call(); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Errorf("%s the returned order: %v", name, err)
		}
	}
	if v := e.get(t, buyer, first.ID); v.Order.ID != second.Order.ID || v.Order.Status != domain.OrderShipped || v.Status != domain.StatusAgreed {
		t.Fatalf("the second order moved: %+v %s", v.Order, v.Status)
	}
	if st, held := e.summary(t, listing); st != catalogdomain.StatusReserved || held != second.Order.ID {
		t.Fatalf("listing %s for %s", st, held)
	}
}

func TestShipRefusesAHiddenListing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.requested(t, buyer, listing)
	if _, err := e.catalog.Hide(ctx, seller, listing); err != nil {
		t.Fatal(err)
	}
	if _, err := e.deals.Ship(ctx, seller, v.Order.ID, key(), spx); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Fatalf("ship: %v", err)
	}
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderRequested {
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

func TestShipLosesToACancel(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.requested(t, buyer, listing)
	deps := e.deps
	deps.Deals = cancelFirst{Deals: e.repo, fired: &atomic.Bool{}}
	if _, err := start(t, deps).Ship(ctx, seller, v.Order.ID, key(), spx); !errors.Is(err, domain.ErrChanged) {
		t.Fatalf("ship: %v", err)
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
}

func TestShipRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	v := e.requested(t, buyer, catalogtest.Listing(t, e.db, seller))
	if _, err := e.deals.Ship(ctx, buyer, v.Order.ID, key(), spx); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Errorf("buyer ships: %v", err)
	}
	if _, err := e.deals.Ship(ctx, stranger, v.Order.ID, key(), spx); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Errorf("stranger ships: %v", err)
	}
	bad := spx
	bad.Fee = -1
	if _, err := e.deals.Ship(ctx, seller, v.Order.ID, key(), bad); !errors.Is(err, domain.ErrInvalidWaybill) {
		t.Errorf("bad waybill: %v", err)
	}
	for name, call := range map[string]func() error{
		"deliver": func() error { _, err := e.deals.Deliver(ctx, seller, v.Order.ID, key()); return err },
		"return":  func() error { _, err := e.deals.Return(ctx, seller, v.Order.ID, key()); return err },
	} {
		if err := call(); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Errorf("%s a requested order: %v", name, err)
		}
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
		w, err := domain.NewWaybill(domain.CarrierSPX, "", "SPXVN0123", 22_000)
		if err != nil {
			return err
		}
		listing := domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingActive}
		if err = cur.Ship(domain.Seller, cur.Order().ID, w, listing, time.Now()); err != nil {
			return err
		}
		if err = c.Deals.Save(ctx, &cur, uuid.NewV7()); err != nil {
			return err
		}
	}
	return c.Deals.Save(ctx, d, writeID, evs...)
}

func TestShipTwiceKeepsTheListing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.requested(t, buyer, listing)
	deps := e.deps
	deps.Deals = shipFirst{Deals: e.repo, fired: &atomic.Bool{}}
	s, err := start(t, deps).Ship(ctx, seller, v.Order.ID, key(), spx)
	if err != nil || s.Order.Status != domain.OrderShipped {
		t.Fatalf("ship: %+v %v", s.Order, err)
	}
	if st, held := e.summary(t, listing); st != catalogdomain.StatusReserved || held != v.Order.ID {
		t.Fatalf("listing %s for %s", st, held)
	}
}
