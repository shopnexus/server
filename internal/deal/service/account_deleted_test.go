package service_test

import (
	"context"
	"testing"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/deal/domain"
)

func TestAccountDeletedEndsTheDeals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	v := e.pending(t, buyer, catalogtest.Listing(t, e.db, seller))

	open, err := e.deals.OpenOrders(ctx, buyer)
	if err != nil || !open {
		t.Fatalf("open orders: %v %v", open, err)
	}
	for range 2 {
		if err = e.deals.AccountDeleted(ctx, buyer); err != nil {
			t.Fatal(err)
		}
	}
	got := e.get(t, seller, v.ID)
	if got.Status != domain.StatusNegotiating || got.Order == nil || got.Order.Status != domain.OrderCancelled {
		t.Fatalf("after: %+v", got)
	}
	if n := countMoves(moveKinds(got), domain.MoveOrderCancelled); n != 1 {
		t.Fatalf("cancelled %d times", n)
	}
	if open, _ = e.deals.OpenOrders(ctx, buyer); open {
		t.Fatal("an order is still open")
	}
	if err = e.deals.ScrubRecipients(ctx, buyer); err != nil {
		t.Fatal(err)
	}
}
