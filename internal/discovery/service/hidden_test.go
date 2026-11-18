package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/discovery/service"
)

func ids(v service.SearchView) []uuid.UUID {
	out := make([]uuid.UUID, len(v.Items))
	for i := range v.Items {
		out[i] = v.Items[i].ID
	}
	return out
}

// A hidden listing and a blocked seller's listing leave the viewer's
// searches, browsing and the nearby feed; unhiding brings the listing back.
func TestHideAndBlockLeaveSearch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.New(t, e.db)
	hidden := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	blocked := e.put(t, "Xe đạp Giant", "vehicles.bicycle", baDinh)
	kept := e.put(t, "Xe đạp Thống Nhất", "vehicles.bicycle", baDinh)
	if err := e.discovery.Hide(ctx, me, hidden); err != nil {
		t.Fatal(err)
	}
	seller, _, err := e.catalog.Summary(ctx, blocked)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.accounts.Block(ctx, seller.SellerID, me); err != nil { // the seller blocks me: either way counts
		t.Fatal(err)
	}
	for _, in := range []service.SearchInput{
		{Query: "xe dap"},
		{Category: "vehicles.bicycle"},
		{Sort: service.SortNearby, Near: catalogdomain.Near{Lat: baDinh.Lat, Lng: baDinh.Lng}},
	} {
		v, searchErr := e.discovery.Search(ctx, &me, in)
		if searchErr != nil {
			t.Fatal(searchErr)
		}
		got := ids(v)
		if slices.Contains(got, hidden) || slices.Contains(got, blocked) || !slices.Contains(got, kept) {
			t.Fatalf("%+v: %v", in, got)
		}
	}
	v, err := e.discovery.Search(ctx, nil, service.SearchInput{Category: "vehicles.bicycle"})
	if err != nil || !slices.Contains(ids(v), hidden) || !slices.Contains(ids(v), blocked) {
		t.Fatalf("signed out sees everything: %v %v", ids(v), err)
	}
	if err = e.discovery.Unhide(ctx, me, hidden); err != nil {
		t.Fatal(err)
	}
	if v, _ = e.discovery.Search(ctx, &me, service.SearchInput{Category: "vehicles.bicycle"}); !slices.Contains(ids(v), hidden) {
		t.Fatalf("unhidden: %v", ids(v))
	}
}

func TestHideRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.New(t, e.db)
	if err := e.discovery.Hide(ctx, me, uuid.NewV7()); !errors.Is(err, catalogdomain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	mine := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	own, _, _ := e.catalog.Summary(ctx, mine)
	if err := e.discovery.Hide(ctx, own.SellerID, mine); !errors.Is(err, domain.ErrOwnListing) {
		t.Fatalf("own: %v", err)
	}
}
