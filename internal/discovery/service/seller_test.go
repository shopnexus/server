package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// A seller's listings come as search items, newest first: saved for the
// viewer who saved one, and measured only when a position is given.
func TestSellerListings(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, me := accounttest.New(t, e.db), accounttest.New(t, e.db)
	at := func(_ *catalogdomain.ListingInput, r *catalogdomain.Refs) { r.CommunePoint = baDinh }
	older, newer := catalogtest.Listing(t, e.db, seller, at), catalogtest.Listing(t, e.db, seller, at)
	if err := e.discovery.Save(ctx, me, older); err != nil {
		t.Fatal(err)
	}
	here := catalogdomain.Near{Lat: baDinh.Lat, Lng: baDinh.Lng}

	first, err := e.discovery.SellerListings(ctx, &me, seller, here, page.Request{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("first page: %+v %v", first, err)
	}
	if it := first.Items[0]; it.ID != newer || it.Saved || it.DistanceKm == nil || *it.DistanceKm != 0 || it.SellerID != seller {
		t.Fatalf("the newer card: %+v", it)
	}
	rest, err := e.discovery.SellerListings(ctx, &me, seller, here, page.Request{Cursor: *first.NextCursor, Limit: 1})
	if err != nil || len(rest.Items) != 1 || rest.NextCursor != nil {
		t.Fatalf("second page: %+v %v", rest, err)
	}
	if it := rest.Items[0]; it.ID != older || !it.Saved || it.DistanceKm == nil {
		t.Fatalf("the saved card: %+v", it)
	}

	anon, err := e.discovery.SellerListings(ctx, nil, seller, catalogdomain.Near{}, page.Request{})
	if err != nil || len(anon.Items) != 2 {
		t.Fatalf("signed out: %+v %v", anon, err)
	}
	for _, it := range anon.Items {
		if it.Saved || it.DistanceKm != nil {
			t.Fatalf("signed out without a position: %+v", it)
		}
	}
	if _, err = e.discovery.SellerListings(ctx, nil, uuid.NewV7(), catalogdomain.Near{}, page.Request{}); !errors.Is(err, accountdomain.ErrAccountNotFound) {
		t.Fatalf("an unknown seller: %v", err)
	}
	if _, err = e.discovery.SellerListings(ctx, nil, seller, catalogdomain.Near{Commune: "nope"}, page.Request{}); !errors.Is(err, catalogdomain.ErrInvalidPosition) {
		t.Fatalf("an unknown commune: %v", err)
	}
}
