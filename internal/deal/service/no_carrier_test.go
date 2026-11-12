package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/provider/carrier"
)

// noCarrier serves a second deal service on e's database with
// carrier.None, production's carrier.
func (e env) noCarrier(t *testing.T) *service.Deals {
	t.Helper()
	deps := e.deps
	deps.Carrier = carrier.None{}
	return start(t, deps)
}

// Without a carrier no shipment is made, read or corrected, and a linked
// one gives the listing back; the moves that need no carrier work as ever.
func TestNoCarrier(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	none := e.noCarrier(t)
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	unavailable := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, domain.ErrCarrierUnavailable) || apperr.From(err).Kind != apperr.KindUnavailable {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err := none.LookupShipment(ctx, "spx", "SPX1")
	unavailable("a lookup", err)
	_, err = none.CreateShipment(ctx, seller, v.Order.ID, key(), manual("SPX1"))
	unavailable("a manual shipment", err)
	_, err = none.CreateShipment(ctx, seller, uuid.NewV7(), key(), manual("SPX1"))
	unavailable("a manual shipment of an unknown order", err)
	_, err = none.CreateShipment(ctx, seller, v.Order.ID, key(), linked)
	unavailable("a linked shipment", err)
	if st, held := e.summary(t, listing); st != catalogdomain.StatusActive || held != (uuid.UUID{}) {
		t.Fatalf("the linked shipment kept the listing: %s for %s", st, held)
	}
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderPending {
		t.Fatalf("a refused shipment moved the order: %+v", got.Order)
	}

	// An order the fake shipped is corrected nowhere, and confirmed as ever.
	shipped := e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller), "SPXNONE1")
	_, err = none.EditShipment(ctx, seller, shipped.Order.ID, "ghn", "GHN1")
	unavailable("a correction", err)
	if c, confirmErr := none.ConfirmReceipt(ctx, buyer, shipped.Order.ID, key()); confirmErr != nil || c.Order.Status != domain.OrderCompleted {
		t.Fatalf("confirm: %+v %v", c.Order, confirmErr)
	}
	if c, cancelErr := none.Cancel(ctx, buyer, v.ID); cancelErr != nil || c.Order.Status != domain.OrderCancelled {
		t.Fatalf("cancel: %+v %v", c.Order, cancelErr)
	}
	must(t)(none.Accept(ctx, buyer, v.ID, 1_500_000))
	if r, codErr := none.RequestCod(ctx, buyer, v.ID, e.address(t, buyer)); codErr != nil || r.Order.Status != domain.OrderPending {
		t.Fatalf("COD: %+v %v", r.Order, codErr)
	}
}
