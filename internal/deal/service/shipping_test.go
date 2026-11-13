package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/testenv"
	"github.com/shopnexus/server2/internal/provider/carrier"
)

// A seller who has not linked the carrier makes, reads and corrects
// nothing there, and a linked shipment refused so gives the listing back.
func TestAnUnlinkedCarrierIsRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	deps := e.deps
	deps.Shipping = refusing{errShippingNotLinked}
	unlinked := start(t, deps)
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	notLinked := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, domain.ErrCarrierNotLinked) {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err := unlinked.LookupShipment(ctx, seller, "ghtk", "S1")
	notLinked("a lookup", err)
	_, err = unlinked.CreateShipment(ctx, seller, v.Order.ID, key(), manual("SPX1"))
	notLinked("a manual shipment", err)
	_, err = unlinked.CreateShipment(ctx, seller, v.Order.ID, key(), linked)
	notLinked("a linked shipment", err)
	if st, held := e.summary(t, listing); st != catalogdomain.StatusActive || held != (uuid.UUID{}) {
		t.Fatalf("the refused shipment kept the listing: %s for %s", st, held)
	}
	shipped := e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller), "SPXLINK1")
	_, err = unlinked.EditShipment(ctx, seller, shipped.Order.ID, "ghn", "GHN1")
	notLinked("a correction", err)

	// The order is found, and its seller is the caller, before any carrier
	// is asked: a stranger's or the buyer's account links nothing.
	stranger := e.person(t)
	for name, c := range map[string]struct {
		caller uuid.UUID
		want   error
	}{"a stranger": {stranger, domain.ErrOrderNotFound}, "the buyer": {buyer, domain.ErrMoveNotAllowed}} {
		if _, err = unlinked.CreateShipment(ctx, c.caller, v.Order.ID, key(), manual("SPX1")); !errors.Is(err, c.want) {
			t.Errorf("%s ships: %v", name, err)
		}
		if _, err = unlinked.EditShipment(ctx, c.caller, shipped.Order.ID, "ghn", "GHN1"); !errors.Is(err, c.want) {
			t.Errorf("%s corrects: %v", name, err)
		}
	}
}

// recording is the fake of each carrier, keeping what it was asked to make.
type recording struct {
	mu   sync.Mutex
	made []carrier.CreateInput
}

func (r *recording) Create(ctx context.Context, _ uuid.UUID, c carrier.Carrier, in carrier.CreateInput) (carrier.Created, error) {
	r.mu.Lock()
	r.made = append(r.made, in)
	r.mu.Unlock()
	return carrier.Fake{Carrier: c}.Create(ctx, carrier.Account{}, in)
}

func (*recording) Lookup(ctx context.Context, _ uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error) {
	return carrier.Fake{Carrier: c}.Lookup(ctx, carrier.Account{}, code)
}

func (*recording) Cancel(context.Context, uuid.UUID, carrier.Carrier, string) error { return nil }

func TestALinkedShipmentGoesToTheCarrierAskedWithItsPickup(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	rec := &recording{}
	deps := e.deps
	deps.Shipping = rec
	deals := start(t, deps)
	seller, buyer := e.person(t), e.person(t)
	from := e.address(t, seller)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	in := service.ShipmentInput{Mode: "linked", Carrier: "ghn", Service: "express", WeightGrams: 2_000, Pickup: "home", PickupAddressID: from}
	s, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), in)
	if err != nil {
		t.Fatal(err)
	}
	sh := s.Order.Shipment
	if sh.Carrier != domain.CarrierGHN || !strings.HasPrefix(sh.TrackingCode, "GHN-") || sh.Pickup == nil ||
		*sh.Pickup != (domain.ShipmentPickup{Name: "Hân", Phone: "+84912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004"}) {
		t.Fatalf("%+v %+v", sh, sh.Pickup)
	}
	made := rec.made[len(rec.made)-1]
	if made.OrderCode != s.Order.Code || made.COD != 1_500_000 || made.WeightGrams != 2_000 || made.Service != "express" ||
		made.Option != "home" || made.Item == "" || made.Recipient.Name != "Hân" || made.Recipient.Commune == "" ||
		made.Recipient.Province == "" || made.Pickup.Place == nil || made.Pickup.Place.Street != "12 Lý Thường Kiệt" ||
		made.Pickup.Place.Commune != made.Recipient.Commune {
		t.Fatalf("asked %+v %+v", made, made.Pickup.Place)
	}

	point := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	in = service.ShipmentInput{Mode: "linked", Carrier: "ghtk", Service: "standard", WeightGrams: 800, Pickup: "home", PickupPointID: "88256"}
	if s, err = deals.CreateShipment(ctx, seller, point.Order.ID, key(), in); err != nil ||
		*s.Order.Shipment.Pickup != (domain.ShipmentPickup{PointID: "88256"}) {
		t.Fatalf("%+v %v", s.Order.Shipment, err)
	}
	both := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	in.PickupAddressID = from
	if _, err = deals.CreateShipment(ctx, seller, both.Order.ID, key(), in); !errors.Is(err, domain.ErrInvalidShipment) {
		t.Fatalf("a point and an address: %v", err)
	}
	in.PickupPointID, in.PickupAddressID = "", uuid.NewV7()
	if _, err = deals.CreateShipment(ctx, seller, both.Order.ID, key(), in); err == nil {
		t.Fatal("an address the seller does not have")
	}
}

// pickedUp is the fake GHTK, which has the parcel: it refuses to call the
// shipment off, and notes whether the listing was back on sale when asked.
type pickedUp struct {
	carrier.Fake

	e        env
	t        *testing.T
	listing  uuid.UUID
	asked    *atomic.Int32
	released *atomic.Bool
}

func (p pickedUp) Cancel(context.Context, carrier.Account, string) error {
	p.asked.Add(1)
	if st, _ := p.e.summary(p.t, p.listing); st == catalogdomain.StatusActive {
		p.released.Store(true)
	}
	return carrier.ErrNotCancellable
}

// The pickup timeout gives the listing back before it calls the shipment
// off, and a carrier that has the parcel already ends the timeout all the
// same.
func TestPickupTimeoutReleasesBeforeCallingOff(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	c := pickedUp{Carrier: carrier.GHTK, e: e, t: t, listing: listing, asked: &atomic.Int32{}, released: &atomic.Bool{}}
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Pickup = time.Second }), linkedAt{c})
	v := e.pending(t, buyer, listing)
	if _, err := deals.CreateShipment(context.Background(), seller, v.Order.ID, key(), linked); err != nil {
		t.Fatal(err)
	}
	if got := e.until(t, buyer, v.ID, domain.OrderCancelled); got.Order.CancelReason != domain.CancelPickupTimeout {
		t.Fatalf("%+v", got.Order)
	}
	testenv.Eventually(t, 15*time.Second, func() error {
		if c.asked.Load() == 0 {
			return errors.New("not called off yet")
		}
		return nil
	})
	if !c.released.Load() {
		t.Fatal("the shipment was called off before the listing went back on sale")
	}
	time.Sleep(2 * time.Second)
	if n := c.asked.Load(); n != 1 {
		t.Fatalf("called off %d times", n)
	}
}

// reporting is a carrier whose parcels all read the status last set.
type reporting struct {
	carrier.Fake

	status *atomic.Pointer[carrier.Status]
	reads  *atomic.Int32
}

func (r reporting) Lookup(ctx context.Context, a carrier.Account, code string) (carrier.Parcel, error) {
	r.reads.Add(1)
	p, err := r.Fake.Lookup(ctx, a, code)
	p.Status = *r.status.Load()
	return p, err
}

// The poll moves a shipped order as the carrier reads it, does nothing when
// a report came first, and stops once the order is closed.
func TestThePollMovesAShipmentAndStopsWhenItCloses(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	c := reporting{Carrier: carrier.GHTK, status: &atomic.Pointer[carrier.Status]{}, reads: &atomic.Int32{}}
	c.status.Store(new(carrier.StatusPendingPickup))
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Poll = 500 * time.Millisecond }), linkedAt{c})
	seller, buyer := e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	if _, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), service.ShipmentInput{Mode: "manual", Carrier: "ghtk", TrackingCode: "S1.POLL"}); err != nil {
		t.Fatal(err)
	}
	c.status.Store(new(carrier.StatusInTransit))
	testenv.Eventually(t, 15*time.Second, func() error {
		if s := e.get(t, buyer, v.ID).Order.Shipment.Status; s != domain.ShipmentInTransit {
			return fmt.Errorf("shipment %s", s)
		}
		return nil
	})
	// A report gets there first; the poll then reads what the order has.
	if err := deals.ShipmentUpdated(ctx, "S1.POLL", "delivered"); err != nil {
		t.Fatal(err)
	}
	c.status.Store(new(carrier.StatusDelivered))
	moves := len(e.get(t, buyer, v.ID).Moves)
	time.Sleep(1500 * time.Millisecond)
	if got := e.get(t, buyer, v.ID); len(got.Moves) != moves || got.Order.Shipment.Status != domain.ShipmentDelivered {
		t.Fatalf("the poll after the report moved the order: %+v", got.Order)
	}
	must(t)(e.deals.ConfirmReceipt(ctx, buyer, v.Order.ID, key()))
	time.Sleep(time.Second)
	reads := c.reads.Load()
	time.Sleep(1500 * time.Millisecond)
	if c.reads.Load() != reads {
		t.Fatalf("the poll went on after the order closed: %d reads, then %d", reads, c.reads.Load())
	}
}

func TestOpenShipmentsAtAndQuoteFacts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller), "SPXOPEN1")
	for c, want := range map[string]bool{"spx": true, "ghtk": false} {
		if open, err := e.deals.OpenShipmentsAt(ctx, seller, c); err != nil || open != want {
			t.Errorf("%s: %v %v", c, open, err)
		}
	}
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	from := e.address(t, seller)
	to, pickup, price, err := e.deals.QuoteFacts(ctx, seller, v.Order.ID, from)
	if err != nil || to.Name != "Hân" || to.Commune == "" || to.Province == "" || price != 1_500_000 || pickup == nil || pickup.Street != "12 Lý Thường Kiệt" {
		t.Fatalf("%+v %+v %d %v", to, pickup, price, err)
	}
	if _, pickup, _, err = e.deals.QuoteFacts(ctx, seller, v.Order.ID, uuid.Nil()); err != nil || pickup != nil {
		t.Fatalf("no address: %+v %v", pickup, err)
	}
	if _, _, _, err = e.deals.QuoteFacts(ctx, buyer, v.Order.ID, uuid.Nil()); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("the buyer's quote: %v", err)
	}
}
