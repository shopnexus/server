package service_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
)

// The service draws the sample under its own text version, with the
// windows counted back from q.Now, and leaves the excluded seller out.
func TestPriceSampleWindows(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := e.account(t), e.account(t)
	vs := repository.NewVectors(e.db)
	text := embedtest.Axis(domain.TextDims, 0)
	listing := func(seller uuid.UUID, price int64) uuid.UUID {
		id := catalogtest.Listing(t, e.db, seller, func(d *domain.ListingInput, _ *domain.Refs) { d.Price = price })
		if err := vs.PutText(ctx, id, "text-test", []byte{1}, text, time.Now()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	listing(other, 1_000_000)
	for price, ago := range map[int64]string{1_200_000: "170 days", 900_000: "190 days"} {
		id := listing(other, price)
		if _, err := e.db.Exec(ctx, `UPDATE listings SET status = 'sold', sold_at = now() - $2::interval WHERE id = $1`, id, ago); err != nil {
			t.Fatal(err)
		}
	}
	listing(me, 5_000_000)
	q := service.PriceQuery{
		Exclude: me, CategoryCode: "vehicles.bicycle", Condition: domain.ConditionUsed,
		Text: text, TextMin: 0.45, PhotoMin: 0.6, Now: time.Now(),
	}
	got, err := e.catalog.PriceSample(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, func(a, b service.PriceSample) int { return int(a.Price - b.Price) })
	if want := []service.PriceSample{{Price: 1_000_000}, {Price: 1_200_000, Sold: true}}; !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
	q.Text = nil
	if got, err = e.catalog.PriceSample(ctx, q); err != nil || got != nil {
		t.Fatalf("no vector: %v %v", got, err)
	}
}

// A draft embeds with its category's display name; an unknown code stands
// for itself, as in ListingEmbed.
func TestEmbeddingTextOfADraft(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	d := domain.ListingInput{Title: "iPhone 11", CategoryCode: "phones.iphone", Condition: domain.ConditionUsed}
	if got, err := e.catalog.EmbeddingText(ctx, d); err != nil || got != "iPhone 11\niPhone\nĐã dùng" {
		t.Fatalf("%q %v", got, err)
	}
	d.CategoryCode = "phones.nothing"
	if got, err := e.catalog.EmbeddingText(ctx, d); err != nil || got != "iPhone 11\nphones.nothing\nĐã dùng" {
		t.Fatalf("%q %v", got, err)
	}
}
