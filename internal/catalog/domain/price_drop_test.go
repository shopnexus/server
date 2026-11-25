package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/catalog/domain"
)

// reprice edits l to price at, keeping the rest of its content.
func reprice(t *testing.T, l *domain.Listing, price int64, at time.Time) {
	t.Helper()
	d := draft()
	d.Photos, d.Price = photoUploads(*l), price
	if err := l.Edit(d, known, at); err != nil {
		t.Fatal(err)
	}
}

func TestPriceDrop(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	for name, tc := range map[string]struct {
		prices []int64 // after the published 9,500,000, one a day; 0 skips a day
		at     time.Duration
		want   int64 // the previous price shown, 0 for none
	}{
		"no change":                 {nil, 0, 0},
		"a drop":                    {[]int64{9_000_000}, day, 9_500_000},
		"a drop, a week later":      {[]int64{9_000_000}, 8 * day, 0},
		"a drop under 5 percent":    {[]int64{9_100_000}, day, 0},
		"a raise":                   {[]int64{9_900_000}, day, 0},
		"a drop, then a raise":      {[]int64{9_000_000, 9_200_000}, 2 * day, 0},
		"two drops":                 {[]int64{9_000_000, 8_500_000}, 2 * day, 9_000_000},
		"a raise, then back":        {[]int64{11_000_000, 9_500_000}, 2 * day, 0},
		"a raise, then below":       {[]int64{11_000_000, 9_000_000}, 2 * day, 9_500_000},
		"two raises, then below":    {[]int64{10_000_000, 11_000_000, 9_000_000}, 3 * day, 9_500_000},
		"a raise that stood a week": {[]int64{11_000_000, 0, 0, 0, 0, 0, 0, 0, 10_000_000}, 9 * day, 11_000_000},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l := published(t)
			for i, p := range tc.prices {
				if p != 0 {
					reprice(t, &l, p, now.Add(time.Duration(i+1)*day))
				}
			}
			got, ok := l.PriceDrop(now.Add(tc.at))
			if tc.want == 0 {
				if ok {
					t.Fatalf("shows %+v", got)
				}
				return
			}
			if !ok || got.PreviousPrice != tc.want {
				t.Fatalf("shows %+v %v, want %d", got, ok, tc.want)
			}
		})
	}
}

func TestPriceDropAt(t *testing.T) {
	t.Parallel()
	l := published(t)
	at := now.Add(time.Hour)
	reprice(t, &l, 8_000_000, at)
	got, ok := l.PriceDrop(at.Add(domain.PriceDropShownFor - time.Second))
	if !ok || !got.DroppedAt.Equal(at) || got.PreviousPrice != 9_500_000 {
		t.Fatalf("%+v %v", got, ok)
	}
	back := domain.RestoreListing(l.State())
	if again, ok := back.PriceDrop(at); !ok || again != got {
		t.Fatalf("the state lost the drop: %+v %v", again, ok)
	}
}

// A price set before the listing first went on sale was never shown.
func TestPriceDropBeforePublication(t *testing.T) {
	t.Parallel()
	l := newListing(t)
	reprice(t, &l, 8_000_000, now)
	if got, ok := l.PriceDrop(now); ok {
		t.Fatalf("shows %+v", got)
	}
}

// Reprice changes the price alone and keeps the drop an edit would show;
// a price out of range or a listing past sale refuses.
func TestReprice(t *testing.T) {
	t.Parallel()
	l := published(t)
	at := now.Add(time.Hour)
	if changed, err := l.Reprice(l.Price(), at); err != nil || changed {
		t.Fatalf("the same price: %v %v", changed, err)
	}
	title := l.Title()
	if changed, err := l.Reprice(9_000_000, at); err != nil || !changed || l.Price() != 9_000_000 || l.Title() != title || !l.UpdatedAt().Equal(at) {
		t.Fatalf("a drop: %v %v %+v", changed, err, l.State())
	}
	if d, ok := l.PriceDrop(at); !ok || d.PreviousPrice != 9_500_000 {
		t.Fatalf("drop %+v %v", d, ok)
	}
	if _, err := l.Reprice(domain.MinPrice-1, at); !errors.Is(err, domain.ErrInvalidListing) {
		t.Fatalf("too low: %v", err)
	}
	if err := l.MarkSold(at); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reprice(8_000_000, at); !errors.Is(err, domain.ErrStateConflict) {
		t.Fatalf("sold: %v", err)
	}
}
