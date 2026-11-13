package service_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/platform/testenv"
	"github.com/shopnexus/server2/internal/provider/carrier"
)

// timed serves a second deal service on e's database with the waits w and
// the carrier c, and returns it with fire, which runs one of its
// OrderTimers handlers now, as Restate does when a wait ends.
func (e env) timed(t *testing.T, w domain.Waits, c service.Shipping) (deals *service.Deals, fire func(handler string, ref service.OrderRef)) {
	t.Helper()
	deps := e.deps
	deps.Waits, deps.Shipping, deps.Prefix = w, c, restatetest.Prefix()
	d := service.New(deps)
	in := restatetest.Start(t, d.Durable()...)
	return d, func(handler string, ref service.OrderRef) {
		t.Helper()
		if _, err := ingress.Service[service.OrderRef, restate.Void](in, deps.Prefix+"OrderTimers", handler).
			Request(context.Background(), ref); err != nil {
			t.Fatalf("%s: %v", handler, err)
		}
	}
}

// soon is domain.OrderWaits changed by set, for a test that waits for
// one timer.
func soon(set func(*domain.Waits)) domain.Waits {
	w := domain.OrderWaits
	set(&w)
	return w
}

// deliveredCarrier is the fake carrier reading every parcel delivered, as
// a carrier does for a code the seller typed after the parcel arrived.
type deliveredCarrier struct{ carrier.Fake }

func (deliveredCarrier) Lookup(ctx context.Context, a carrier.Account, code string) (carrier.Parcel, error) {
	p, err := carrier.Fake{}.Lookup(ctx, a, code)
	p.Status = carrier.StatusDelivered
	return p, err
}

// until reads deal id as viewer until its order has status.
func (e env) until(t *testing.T, viewer, id uuid.UUID, status domain.OrderStatus) service.DealView {
	t.Helper()
	var v service.DealView
	testenv.Eventually(t, 15*time.Second, func() error {
		if v = e.get(t, viewer, id); v.Order.Status != status {
			return fmt.Errorf("order %s, want %s", v.Order.Status, status)
		}
		return nil
	})
	return v
}

func TestSellerTimeoutCancels(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Seller = time.Second }), linkedAt{})
	seller, buyer := e.person(t), e.person(t)
	v := e.agreed(t, buyer, catalogtest.Listing(t, e.db, seller))
	if _, err := deals.RequestCod(context.Background(), buyer, v.ID, e.address(t, buyer)); err != nil {
		t.Fatal(err)
	}
	got := e.until(t, seller, v.ID, domain.OrderCancelled)
	if got.Order.CancelledBy != domain.System || got.Order.CancelReason != domain.CancelSellerTimeout ||
		got.Status != domain.StatusNegotiating || got.AgreedPrice != nil {
		t.Fatalf("%+v", got)
	}
}

func TestPickupTimeoutCancels(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	cancels := &atomic.Int32{}
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Pickup = time.Second }), linkedAt{countingCarrier{Carrier: carrier.GHTK, cancels: cancels}})
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	if _, err := deals.CreateShipment(context.Background(), seller, v.Order.ID, key(), linked); err != nil {
		t.Fatal(err)
	}
	got := e.until(t, buyer, v.ID, domain.OrderCancelled)
	if got.Order.CancelledBy != domain.System || got.Order.CancelReason != domain.CancelPickupTimeout ||
		got.Order.Shipment == nil || got.Order.Shipment.Mode != domain.ModeLinked || got.Status != domain.StatusNegotiating {
		t.Fatalf("%+v", got)
	}
	testenv.Eventually(t, 15*time.Second, func() error {
		if st, _ := e.summary(t, listing); st != catalogdomain.StatusActive {
			return fmt.Errorf("listing %s", st)
		}
		return nil
	})
	if cancels.Load() != 1 {
		t.Fatalf("%d cancels at GHTK", cancels.Load())
	}
}

func TestAutoCompletes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Complete = time.Second }), linkedAt{})
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	if _, err := deals.CreateShipment(ctx, seller, v.Order.ID, key(), manual("SPXAUTO1")); err != nil {
		t.Fatal(err)
	}
	if err := deals.ShipmentUpdated(ctx, "SPXAUTO1", "delivered"); err != nil {
		t.Fatal(err)
	}
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderShipped || got.Order.Shipment.Status != domain.ShipmentDelivered ||
		got.Order.Shipment.DeliveredAt == nil {
		t.Fatalf("delivered: %+v", got.Order)
	}
	got := e.until(t, seller, v.ID, domain.OrderCompleted)
	if m := got.Moves[len(got.Moves)-1]; m.Kind != domain.MoveCompleted || m.By != domain.System || got.Status != domain.StatusSold {
		t.Fatalf("%+v %s", m, got.Status)
	}
	testenv.Eventually(t, 15*time.Second, func() error {
		if st, _ := e.summary(t, listing); st != catalogdomain.StatusSold {
			return fmt.Errorf("listing %s", st)
		}
		return nil
	})
}

func TestShipADeliveredParcelCompletes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Complete = time.Second }), linkedAt{deliveredCarrier{}})
	seller, buyer := e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	s, err := deals.CreateShipment(context.Background(), seller, v.Order.ID, key(), manual("SPXLATE1"))
	if err != nil || s.Order.Shipment.Status != domain.ShipmentDelivered || s.Order.Shipment.DeliveredAt == nil {
		t.Fatalf("%+v %v", s.Order, err)
	}
	e.until(t, buyer, v.ID, domain.OrderCompleted)
}

func TestTimersLeaveAnOrderThatMovedOn(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	deals, fire := e.timed(t, domain.OrderWaits, linkedAt{})
	seller, buyer := e.person(t), e.person(t)
	ref := func(v service.DealView) service.OrderRef { return service.OrderRef{DealID: v.ID, OrderID: v.Order.ID} }
	unchanged := func(name string, v service.DealView) {
		t.Helper()
		got := e.get(t, buyer, v.ID)
		if got.Order.ID != v.Order.ID || got.Order.Status != v.Order.Status || len(got.Moves) != len(v.Moves) {
			t.Errorf("%s moved: %+v", name, got.Order)
		}
	}

	shipped := e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller), "SPXMOVED1")
	fire("SellerTimeout", ref(shipped))
	fire("AutoComplete", ref(shipped))
	unchanged("a shipment waiting for pickup", shipped)

	if err := deals.ShipmentUpdated(ctx, "SPXMOVED1", "in_transit"); err != nil {
		t.Fatal(err)
	}
	transit := e.get(t, buyer, shipped.ID)
	fire("PickupTimeout", ref(transit))
	fire("AutoComplete", ref(transit))
	unchanged("a parcel in transit", transit)

	if err := deals.ShipmentUpdated(ctx, "SPXMOVED1", "delivered"); err != nil {
		t.Fatal(err)
	}
	must(t)(e.deals.ConfirmReceipt(ctx, buyer, shipped.Order.ID, key()))
	confirmed := e.get(t, buyer, shipped.ID)
	fire("AutoComplete", ref(confirmed))
	fire("PickupTimeout", ref(confirmed))
	unchanged("a confirmed order", confirmed)

	// The deal's first order was cancelled and the buyer asked again: the
	// first order's timer leaves the second alone, and the second's own
	// timer cancels it.
	first := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	must(t)(e.deals.Cancel(ctx, buyer, first.ID))
	must(t)(e.deals.Accept(ctx, buyer, first.ID, 1_500_000))
	second, err := e.deals.RequestCod(ctx, buyer, first.ID, e.address(t, buyer))
	if err != nil {
		t.Fatal(err)
	}
	fire("SellerTimeout", ref(first))
	unchanged("an order asked for again", second)
	fire("SellerTimeout", ref(second))
	if got := e.get(t, buyer, second.ID); got.Order.Status != domain.OrderCancelled || got.Order.CancelReason != domain.CancelSellerTimeout {
		t.Fatalf("the second order's own timer: %+v", got.Order)
	}
}

// Production runs without a carrier (CARRIER=none): the timers and the
// carrier's entry point need none.
func TestTimersWorkWithoutACarrier(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	deals, _ := e.timed(t, soon(func(w *domain.Waits) { w.Seller = time.Second }), refusing{errShippingUnavailable})
	seller, buyer := e.person(t), e.person(t)
	v := e.agreed(t, buyer, catalogtest.Listing(t, e.db, seller))
	if _, err := deals.RequestCod(ctx, buyer, v.ID, e.address(t, buyer)); err != nil {
		t.Fatal(err)
	}
	if got := e.until(t, buyer, v.ID, domain.OrderCancelled); got.Order.CancelReason != domain.CancelSellerTimeout {
		t.Fatalf("%+v", got.Order)
	}
	shipped := e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller), "SPXNONE2")
	if err := deals.ShipmentUpdated(ctx, "SPXNONE2", "failed"); err != nil {
		t.Fatal(err)
	}
	if got := e.get(t, buyer, shipped.ID); got.Order.Status != domain.OrderFailed {
		t.Fatalf("%+v", got.Order)
	}
}

func TestNewRefusesAZeroWait(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("New took a zero wait")
		}
	}()
	service.New(service.Deps{Waits: domain.Waits{Seller: time.Hour, Complete: time.Hour}})
}
