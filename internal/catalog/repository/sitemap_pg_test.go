package repository_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// The sitemap reads only active and reserved listings, cuts them and their
// sellers into parts newest first, and each part's cursor reads exactly
// that part.
func TestSitemapParts(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewListings(db)
	ctx := context.Background()
	older, newer := accounttest.New(t, db), accounttest.New(t, db)
	// put inserts a listing of seller and moves it at minute m.
	put := func(seller uuid.UUID, m int, move func(*domain.Listing, time.Time)) domain.Listing {
		t.Helper()
		l := listing(t, seller)
		if err := s.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
		if move != nil {
			move(&l, now.Add(time.Duration(m)*time.Minute))
			if err := s.Save(ctx, &l, uuid.NewV7(), nil); err != nil {
				t.Fatal(err)
			}
		}
		return l
	}
	publish := func(l *domain.Listing, at time.Time) { l.Published(nil, at) }
	a := put(older, 1, publish)
	put(older, 2, func(l *domain.Listing, at time.Time) {
		l.Published(nil, at)
		if err := l.Hide(at); err != nil {
			t.Fatal(err)
		}
	})
	b := put(older, 3, func(l *domain.Listing, at time.Time) {
		l.Published(nil, at)
		if _, err := l.Reserve(uuid.NewV7(), at); err != nil {
			t.Fatal(err)
		}
	})
	c := put(newer, 4, publish)
	put(newer, 5, nil) // still processing
	minute := func(m int) time.Time { return now.Add(time.Duration(m) * time.Minute) }
	// code is the listing's code, which the insert picks.
	code := func(l domain.Listing) string {
		t.Helper()
		got, err := s.Get(ctx, l.ID())
		if err != nil {
			t.Fatal(err)
		}
		return got.State().Code
	}

	parts, err := s.ListingParts(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []repository.SitemapPart{{UpdatedAt: minute(4)}, {Cursor: b.ID(), UpdatedAt: minute(1)}}
	if !slices.EqualFunc(parts, want, samePart) {
		t.Fatalf("listing parts: %+v, want %+v", parts, want)
	}
	first, err := s.SitemapListings(ctx, uuid.Max(), 2)
	if err != nil || len(first) != 2 || first[0].Code != code(c) || first[1].Code != code(b) || first[0].Title != c.Title() {
		t.Fatalf("first part: %+v %v", first, err)
	}
	rest, err := s.SitemapListings(ctx, parts[1].Cursor, 2)
	if err != nil || len(rest) != 1 || rest[0].Code != code(a) || !rest[0].UpdatedAt.Equal(minute(1)) {
		t.Fatalf("second part: %+v %v", rest, err)
	}

	sellerParts, err := s.SellerParts(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantSellers := []repository.SitemapPart{{UpdatedAt: minute(4)}, {Cursor: newer, UpdatedAt: minute(3)}}
	if !slices.EqualFunc(sellerParts, wantSellers, samePart) {
		t.Fatalf("seller parts: %+v, want %+v", sellerParts, wantSellers)
	}
	sellers, err := s.SitemapSellers(ctx, sellerParts[1].Cursor, 1)
	if err != nil || len(sellers) != 1 || sellers[0].ID != older || !sellers[0].UpdatedAt.Equal(minute(3)) {
		t.Fatalf("second seller part: %+v %v", sellers, err)
	}
}

func samePart(a, b repository.SitemapPart) bool {
	return a.Cursor == b.Cursor && a.UpdatedAt.Equal(b.UpdatedAt)
}
