package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
)

// MarketPrice compares a listing with the listings like it, its seller's
// own left out.
func TestMarketPrice(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := e.account(t), e.account(t)
	vs := repository.NewVectors(e.db)
	text := embedtest.Axis(domain.TextDims, 0)
	listing := func(seller uuid.UUID, price int64) uuid.UUID {
		id := catalogtest.Listing(t, e.db, seller, func(d *domain.Draft, _ *domain.Refs) { d.Price = price })
		if err := vs.PutText(ctx, id, "text-test", []byte{1}, text, time.Now()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	for _, p := range []int64{9_000_000, 10_000_000, 10_000_000, 11_000_000, 12_000_000} {
		listing(other, p)
	}
	listing(me, 10_000) // the seller's own listing stays out of the sample
	mine := listing(me, 6_000_000)

	got, ok, err := e.catalog.MarketPrice(ctx, mine, 0.45, 0.6)
	if err != nil || !ok || got.SellerID != me || got.Price != 6_000_000 || got.Median != 10_000_000 || got.SampleSize != 5 {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	if _, ok, err = e.catalog.MarketPrice(ctx, uuid.NewV7(), 0.45, 0.6); err != nil || ok {
		t.Fatalf("unknown: %v %v", ok, err)
	}
}
