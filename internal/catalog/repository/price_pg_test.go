package repository_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/provider/embed/embedtest"
)

// priced puts a used bicycle of seller's on sale at price, changed by
// change, with a text vector and a photo vector under t1 and p1; nil puts
// none.
func priced(t *testing.T, db *pgxpool.Pool, seller uuid.UUID, price int64, text, photo []float32,
	change ...func(*domain.ListingInput, *domain.Refs),
) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := catalogtest.Listing(t, db, seller, append([]func(*domain.ListingInput, *domain.Refs){
		func(d *domain.ListingInput, _ *domain.Refs) { d.Price = price },
	}, change...)...)
	vs := repository.NewVectors(db)
	if text != nil {
		if err := vs.PutText(ctx, id, "t1", []byte{1}, text, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if photo != nil {
		if err := vs.PutPhoto(ctx, id, "media/a.jpg", "p1", photo, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func exec(t *testing.T, db *pgxpool.Pool, sql string, id uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql, id); err != nil {
		t.Fatal(err)
	}
}

func prices(samples []domain.PriceSample) []int64 {
	out := make([]int64, len(samples))
	for i, s := range samples {
		out[i] = s.Price
	}
	return out
}

// A price sample takes listings of the category and condition, sold within
// 180 days or on sale and published within 90, at or above a floor on its
// text or its best photo, under the current versions, not the excluded
// seller's; the most similar first.
func TestPriceSample(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	vs := repository.NewVectors(db)
	me, seller := accounttest.New(t, db), accounttest.New(t, db)
	axis, off := embedtest.Axis(domain.TextDims, 0), embedtest.Axis(domain.TextDims, 1)
	photo := embedtest.Axis(domain.PhotoDims, 0)

	same := priced(t, db, seller, 1_000_000, axis, nil, func(_ *domain.ListingInput, r *domain.Refs) {
		r.CommunePoint = domain.Point{Lat: 21.0394, Lng: 105.8364}
	}) // the same text
	priced(t, db, seller, 1_100_000, embedtest.Near(domain.TextDims, 0, 1, 0.8), nil)
	priced(t, db, seller, 1_200_000, off, photo)                                      // through its photo
	priced(t, db, seller, 1_300_000, embedtest.Near(domain.TextDims, 0, 1, 0.3), nil) // under the floor
	sold := priced(t, db, seller, 1_400_000, axis, nil)
	exec(t, db, `UPDATE listings SET status = 'sold', sold_at = now() - interval '10 days',
    published_at = now() - interval '10 days' - interval '36 hours' WHERE id = $1`, sold)
	soldLongAgo := priced(t, db, seller, 1_500_000, axis, nil)
	exec(t, db, `UPDATE listings SET status = 'sold', sold_at = now() - interval '200 days' WHERE id = $1`, soldLongAgo)
	old := priced(t, db, seller, 1_600_000, axis, nil)
	exec(t, db, `UPDATE listings SET published_at = now() - interval '100 days' WHERE id = $1`, old)
	hidden := priced(t, db, seller, 1_700_000, axis, nil)
	exec(t, db, `UPDATE listings SET status = 'hidden' WHERE id = $1`, hidden)
	reserved := priced(t, db, seller, 1_800_000, embedtest.Near(domain.TextDims, 0, 1, 0.9), nil)
	exec(t, db, `UPDATE listings SET status = 'reserved', reserved_for = gen_random_uuid() WHERE id = $1`, reserved)
	priced(t, db, seller, 1_900_000, axis, nil, func(d *domain.ListingInput, _ *domain.Refs) { d.Condition = domain.ConditionLikeNew })
	priced(t, db, seller, 2_000_000, axis, nil, func(d *domain.ListingInput, _ *domain.Refs) { d.CategoryCode = "phones.iphone" })
	priced(t, db, me, 2_100_000, axis, nil)
	stale := priced(t, db, seller, 2_200_000, nil, nil)
	if err := vs.PutText(ctx, stale, "t0", []byte{1}, axis, time.Now()); err != nil {
		t.Fatal(err)
	}

	at := time.Now()
	f := repository.PriceFilter{
		Exclude: me, CategoryCode: "vehicles.bicycle", Condition: domain.ConditionUsed,
		Text: axis, TextVersion: "t1", TextMin: 0.45, Photo: photo, PhotoVersion: "p1", PhotoMin: 0.6,
		SoldSince: at.Add(-domain.PriceSoldWindow), OnSaleSince: at.Add(-domain.PriceOnSaleWindow), Limit: 30,
	}
	got, err := vs.PriceSample(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{1_000_000, 1_200_000, 1_400_000, 1_800_000, 1_100_000}; !slices.Equal(prices(got), want) {
		t.Fatalf("%v, want %v", prices(got), want)
	}
	for _, s := range got {
		if s.Sold != (s.Price == 1_400_000) {
			t.Fatalf("sold flag: %+v", s)
		}
	}
	for _, s := range got {
		switch s.Price {
		case 1_400_000:
			if s.ListingID != sold || s.DaysToSell != 2 {
				t.Fatalf("sold: %+v", s)
			}
		case 1_000_000:
			if s.ListingID != same || s.DaysToSell != 0 || s.Point != (domain.Point{Lat: 21.0394, Lng: 105.8364}) {
				t.Fatalf("on sale: %+v", s)
			}
		default:
		}
	}
	f.Limit = 2
	if got, _ = vs.PriceSample(ctx, f); !slices.Equal(prices(got), []int64{1_000_000, 1_200_000}) {
		t.Fatalf("limit: %v", prices(got))
	}
	f.Limit, f.Text = 30, nil
	if got, _ = vs.PriceSample(ctx, f); !slices.Equal(prices(got), []int64{1_200_000}) {
		t.Fatalf("photo alone: %v", prices(got))
	}
	f.Photo = nil
	if got, err = vs.PriceSample(ctx, f); err != nil || got != nil {
		t.Fatalf("no vector: %v %v", got, err)
	}
}
