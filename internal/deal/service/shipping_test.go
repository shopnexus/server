package service_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/testenv"
	"github.com/shopnexus/server/internal/provider/carrier"
	shippingdomain "github.com/shopnexus/server/internal/shipping/domain"
	shippingrepo "github.com/shopnexus/server/internal/shipping/repository"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
)

// A seller who has not linked the carrier makes, reads and corrects
// nothing there, and a linked shipment refused so gives the listing back.
//
// UC-DEAL-04 E3.
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

func (r *recording) Track(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error) {
	return r.Lookup(ctx, seller, c, code)
}

func (*recording) Cancel(context.Context, uuid.UUID, carrier.Carrier, string) error { return nil }

// UC-DEAL-04 · ADR-0001.
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
	// The pickup is the seller's address: the buyer does not read it.
	if b := e.get(t, buyer, s.ID).Order.Shipment; b.Pickup != nil {
		t.Fatalf("the buyer reads the seller's pickup %+v", b.Pickup)
	}
	made := rec.made[len(rec.made)-1]
	if made.OrderCode != s.Order.Code || made.COD != 1_500_000 || made.WeightGrams != 2_000 || made.Service != "express" ||
		made.Option != "home" || made.Item == "" || made.Recipient.Name != "Hân" || made.Recipient.Commune == "" ||
		made.Recipient.Province == "" || made.Pickup.Place == nil || made.Pickup.Place.Street != "12 Lý Thường Kiệt" ||
		made.Pickup.Place.Commune != made.Recipient.Commune || made.Note != "Nhà riêng" {
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

// The recipient's kind of address reaches the carrier as the note to its
// shipper: an office takes parcels in office hours, other says nothing.
func TestALinkedShipmentTellsTheShipperWhatThePlaceIs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	rec := &recording{}
	deps := e.deps
	deps.Shipping = rec
	deals := start(t, deps)
	seller := e.person(t)
	for kind, note := range map[accountdomain.AddressKind]string{
		accountdomain.AddressHome:  "Nhà riêng",
		accountdomain.AddressWork:  "Địa chỉ công ty: giao giờ hành chính",
		accountdomain.AddressOther: "",
	} {
		buyer := e.person(t)
		draft := home
		draft.Kind = kind
		a, err := e.accounts.CreateAddress(ctx, buyer, uuid.NewV7(), draft, false)
		if err != nil {
			t.Fatal(err)
		}
		v := e.agreed(t, buyer, catalogtest.Listing(t, e.db, seller))
		if v, err = e.deals.RequestCod(ctx, buyer, v.ID, a.ID, ""); err != nil || v.Order.Recipient.Kind != domain.AddressKind(kind) {
			t.Fatalf("%s: %+v %v", kind, v.Order, err)
		}
		in := service.ShipmentInput{Mode: "linked", Carrier: "ghtk", Service: "standard", WeightGrams: 800, Pickup: "home"}
		if _, err = deals.CreateShipment(ctx, seller, v.Order.ID, key(), in); err != nil {
			t.Fatal(err)
		}
		rec.mu.Lock()
		var made carrier.CreateInput
		for _, m := range rec.made {
			if m.OrderCode == v.Order.Code {
				made = m
			}
		}
		rec.mu.Unlock()
		if made.OrderCode == "" || made.Note != note {
			t.Errorf("%s: note %q, want %q", kind, made.Note, note)
		}
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
//
// UC-DEAL-06 A2.
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

// reporting is a carrier whose parcels all read the step last set; a nil
// step is a status it does not map, which moves nothing.
type reporting struct {
	carrier.Fake

	step  *atomic.Pointer[carrier.Step]
	reads *atomic.Int32
}

func (r reporting) Lookup(ctx context.Context, a carrier.Account, code string) (carrier.Parcel, error) {
	r.reads.Add(1)
	p, err := r.Fake.Lookup(ctx, a, code)
	if s := r.step.Load(); s != nil {
		p.Step = *s
	}
	return p, err
}

// A status the carrier does not map (GHTK's delayed delivery, GHN's
// exception) moves nothing: the poll leaves a delivered parcel delivered,
// and it completes.
//
// UC-DEAL-05.
func TestThePollIgnoresAStatusThatMovesNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	c := reporting{Carrier: carrier.GHTK, step: &atomic.Pointer[carrier.Step]{}, reads: &atomic.Int32{}}
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Poll, w.Complete = 300*time.Millisecond, 3*time.Second }), linkedAt{c})
	seller, buyer := e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	if _, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), service.ShipmentInput{Mode: "manual", Carrier: "ghtk", TrackingCode: "S1.LATE"}); err != nil {
		t.Fatal(err)
	}
	if err := deals.ShipmentUpdated(ctx, "S1.LATE", "delivered"); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, 15*time.Second, func() error {
		if c.reads.Load() < 2 {
			return errors.New("not polled twice yet")
		}
		return nil
	})
	if s := e.get(t, buyer, v.ID).Order.Shipment.Status; s != domain.ShipmentDelivered {
		t.Fatalf("the poll moved a delivered parcel to %s", s)
	}
	e.until(t, buyer, v.ID, domain.OrderCompleted)
}

// The poll moves a shipped order as the carrier reads it, does nothing when
// a report came first, and stops once the order is closed.
func TestThePollMovesAShipmentAndStopsWhenItCloses(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	c := reporting{Carrier: carrier.GHTK, step: &atomic.Pointer[carrier.Step]{}, reads: &atomic.Int32{}}
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Poll = 500 * time.Millisecond }), linkedAt{c})
	seller, buyer := e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	if _, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), service.ShipmentInput{Mode: "manual", Carrier: "ghtk", TrackingCode: "S1.POLL"}); err != nil {
		t.Fatal(err)
	}
	c.step.Store(new(carrier.StepTransporting))
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
	c.step.Store(new(carrier.StepDelivered))
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

// A parcel the carrier picked up after the last poll is not cancelled when
// the pickup wait ends: the timeout reads it first and moves the order.
func TestPickupTimeoutReadsTheCarrierFirst(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	c := reporting{Carrier: carrier.GHTK, step: &atomic.Pointer[carrier.Step]{}, reads: &atomic.Int32{}}
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Pickup = time.Second }), linkedAt{c})
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	if _, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), linked); err != nil {
		t.Fatal(err)
	}
	c.step.Store(new(carrier.StepTransporting))
	testenv.Eventually(t, 15*time.Second, func() error {
		if s := e.get(t, buyer, v.ID).Order.Shipment.Status; s != domain.ShipmentInTransit {
			return fmt.Errorf("shipment %s", s)
		}
		return nil
	})
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderShipped {
		t.Fatalf("the picked-up order was cancelled: %+v", got.Order)
	}
	if st, held := e.summary(t, listing); st == catalogdomain.StatusActive || held != v.Order.ID {
		t.Fatalf("the listing went back on sale: %s for %s", st, held)
	}
}

// A carrier's refusal ends the shipment at once with the carrier's reason
// and gives the listing back; it is not tried again.
func TestACarriersRefusalGivesTheListingBack(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	deps := e.deps
	deps.Shipping = refusing{apperr.New(apperr.KindInvalid, "carrier_refused", "the carrier refused the request").WithReason("bad phone")}
	deals := start(t, deps)
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	_, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), linked)
	if apperr.From(err).Code != "carrier_refused" {
		t.Fatalf("%v", err)
	}
	if st, held := e.summary(t, listing); st != catalogdomain.StatusActive || held != (uuid.UUID{}) {
		t.Fatalf("the refused shipment kept the listing: %s for %s", st, held)
	}
}

func TestSavedPickupShipmentReplayAfterPreferenceRemoval(t *testing.T) {
	t.Parallel()
	for _, removeAddress := range []bool{false, true} {
		name := "clear_preference"
		if removeAddress {
			name = "delete_address"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			ctx := t.Context()
			seller, buyer := e.person(t), e.person(t)
			address := e.address(t, seller)
			tokenKey := make([]byte, 32)
			if _, err := rand.Read(tokenKey); err != nil {
				t.Fatal(err)
			}
			shipping := shippingsvc.New(shippingsvc.Deps{
				Accounts: shippingrepo.NewAccounts(e.db), Settings: shippingrepo.NewSettings(e.db), Customers: e.accounts,
				Clients: map[carrier.Carrier]carrier.Client{carrier.GHTK: carrier.Fake{Carrier: carrier.GHTK}}, Key: tokenKey, Now: time.Now,
			})
			if _, err := shipping.Link(ctx, seller, "ghtk", carrier.Credentials{Email: "seller@test.invalid", Password: "test"}); err != nil {
				t.Fatal(err)
			}
			if _, err := shipping.PutSettings(ctx, seller, shippingdomain.Settings{Pickup: &shippingdomain.SavedPickup{AddressID: address, Phone: "0987654321"}}); err != nil {
				t.Fatal(err)
			}
			deps := e.deps
			deps.Shipping = shipping
			deals := start(t, deps)
			pending := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
			in := linked
			in.SavedPickup = true
			requestKey := key()
			first, err := deals.CreateShipment(ctx, seller, pending.Order.ID, requestKey, in)
			if err != nil {
				t.Fatal(err)
			}
			if removeAddress {
				if err = e.accounts.DeleteAddress(ctx, seller, address); err != nil {
					t.Fatal(err)
				}
			} else if _, err = shipping.PutSettings(ctx, seller, shippingdomain.Settings{}); err != nil {
				t.Fatal(err)
			}
			recovered, err := deals.CreateShipment(ctx, seller, pending.Order.ID, requestKey, in)
			if err != nil {
				t.Fatalf("same key/body after lost response and removed preference: %v", err)
			}
			if recovered.Order.ID != first.Order.ID || recovered.Order.Status != domain.OrderShipped || recovered.Order.Shipment.TrackingCode != first.Order.Shipment.TrackingCode || recovered.Order.Shipment.Pickup == nil || *recovered.Order.Shipment.Pickup != *first.Order.Shipment.Pickup {
				t.Fatalf("replay changed shipment: %+v", recovered.Order)
			}
			if n := countMoves(moveKinds(e.get(t, seller, pending.ID)), domain.MoveShipped); n != 1 {
				t.Fatalf("shipped %d times", n)
			}
		})
	}
}
