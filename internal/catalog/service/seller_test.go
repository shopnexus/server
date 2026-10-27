package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

// A seller's public list holds the active and reserved listings, newest
// first, and the counts set them apart from the sold ones.
func TestSellerListingsAndCounts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	sold := e.active(t, seller).ID
	if _, err := e.catalog.MarkSold(ctx, seller, sold); err != nil {
		t.Fatal(err)
	}
	hidden := e.active(t, seller).ID
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	active := e.active(t, seller).ID
	reserved := e.active(t, seller).ID
	if err := e.catalog.Reserve(ctx, reserved, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	first, err := e.catalog.SellerListings(ctx, seller, page.Request{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != reserved || first.NextCursor == nil {
		t.Fatalf("first page: %+v %v", first, err)
	}
	rest, err := e.catalog.SellerListings(ctx, seller, page.Request{Cursor: *first.NextCursor, Limit: 1})
	if err != nil || len(rest.Items) != 1 || rest.Items[0].ID != active || rest.NextCursor != nil {
		t.Fatalf("second page: %+v %v", rest, err)
	}
	if got, err := e.catalog.SellerCounts(ctx, seller); err != nil || got != (service.SellerCounts{Sold: 1, OnSale: 2}) {
		t.Fatalf("counts: %+v %v", got, err)
	}
	if _, err := e.catalog.SellerListings(ctx, uuid.NewV7(), page.Request{}); !errors.Is(err, accountdomain.ErrAccountNotFound) {
		t.Fatalf("unknown seller: %v", err)
	}
}
