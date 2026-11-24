package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/deal/domain"
)

// UC-DEAL-06 · NFR-CONC-01.
func TestCancelSelectedOrderDoesNotCancelItsReplacement(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	d := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))
	stale, err := e.repo.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	must(t)(e.deals.CancelOrder(ctx, buyer, d.Order.ID, ""))
	replacement, err := e.deals.RequestCod(ctx, buyer, d.ID, 1_500_000, e.address(t, buyer), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, who := range []uuid.UUID{buyer, seller} {
		if _, err = e.deals.CancelOrder(ctx, who, d.Order.ID, ""); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Fatalf("historical: %v", err)
		}
	}
	deps := e.deps
	deps.Deals = staleGet{Deals: e.repo, stale: stale}
	staleService := start(t, deps)
	if _, err = staleService.CancelOrder(ctx, buyer, d.Order.ID, ""); !errors.Is(err, domain.ErrChanged) {
		t.Fatalf("stale save: %v", err)
	}
	if current := e.get(t, buyer, d.ID).Order; current.ID != replacement.Order.ID || current.Status != domain.OrderPending {
		t.Fatalf("replacement: %+v", current)
	}
	historical, dealID, err := e.deals.OrderFor(ctx, seller, d.Order.ID)
	if err != nil || historical.Status != domain.OrderCancelled || historical.Recipient.Phone != "" || historical.Recipient.Street != "" || dealID != d.ID {
		t.Fatalf("historical: %+v %v", historical, err)
	}
}
