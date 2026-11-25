package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// noCarrier serves a second deal service on e's database with module
// shipping as it answers with both carriers off, production's.
func (e env) noCarrier(t *testing.T) *service.Deals {
	t.Helper()
	deps := e.deps
	deps.Shipping = refusing{errShippingUnavailable}
	return start(t, deps)
}

// Without a carrier no shipment is made, and the refused one gives the
// listing back; the moves that need no carrier work as ever.
//
// UC-DEAL-04 E2.
func TestNoCarrier(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	none := e.noCarrier(t)
	seller, buyer := e.person(t), e.person(t)
	listing := catalogtest.Listing(t, e.db, seller)
	v := e.pending(t, buyer, listing)
	_, err := none.CreateShipment(ctx, seller, v.Order.ID, key(), linked)
	if !errors.Is(err, domain.ErrCarrierUnavailable) || apperr.From(err).Kind != apperr.KindUnavailable {
		t.Fatalf("a shipment: %v", err)
	}
	if st, held := e.summary(t, listing); st != catalogdomain.StatusActive || held != (uuid.UUID{}) {
		t.Fatalf("the refused shipment kept the listing: %s for %s", st, held)
	}
	if got := e.get(t, buyer, v.ID); got.Order.Status != domain.OrderPending {
		t.Fatalf("a refused shipment moved the order: %+v", got.Order)
	}
	if _, err = none.CreateShipment(ctx, seller, uuid.NewV7(), key(), linked); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("a shipment of an unknown order: %v", err)
	}

	// An order the fake shipped is confirmed as ever.
	shipped := e.shipped(t, buyer, seller, catalogtest.Listing(t, e.db, seller))
	if c, confirmErr := none.ConfirmReceipt(ctx, buyer, shipped.Order.ID, key()); confirmErr != nil || c.Order.Status != domain.OrderCompleted {
		t.Fatalf("confirm: %+v %v", c.Order, confirmErr)
	}
	if c, cancelErr := none.Cancel(ctx, buyer, v.ID); cancelErr != nil || c.Order.Status != domain.OrderCancelled {
		t.Fatalf("cancel: %+v %v", c.Order, cancelErr)
	}
	if r, codErr := none.RequestCod(ctx, buyer, v.ID, 1_500_000, e.address(t, buyer), ""); codErr != nil || r.Order.Status != domain.OrderPending {
		t.Fatalf("COD: %+v %v", r.Order, codErr)
	}
}
