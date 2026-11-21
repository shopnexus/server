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

// A sitemap listing carries its processed photos' keys in order.
func TestSitemapListingPhotos(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewListings(db)
	id, at := uuid.NewV7(), time.Now()
	l := domain.RestoreListing(domain.ListingState{
		ID: id, SellerID: accounttest.New(t, db), Title: "Ghế gỗ", Price: 500_000, CategoryCode: "furniture.other",
		Condition: domain.ConditionUsed, CommuneCode: "00004", Status: domain.StatusActive, WriteID: id,
		Photos: []domain.Photo{
			{UploadID: uuid.NewV7(), Keys: domain.PhotoKeys{Photo: "media/b.jpg", Thumb: "media/b_t.jpg"}},
			{UploadID: uuid.NewV7()},
			{UploadID: uuid.NewV7(), Keys: domain.PhotoKeys{Photo: "media/a.jpg", Thumb: "media/a_t.jpg"}},
		},
		CreatedAt: at, UpdatedAt: at, PublishedAt: &at, Lat: 21.03, Lng: 105.85,
	})
	if err := s.Insert(ctx, l); err != nil {
		t.Fatal(err)
	}
	got, err := s.SitemapListings(ctx, uuid.Max(), 10)
	if err != nil || len(got) != 1 || !slices.Equal(got[0].Photos, []string{"media/b.jpg", "media/a.jpg"}) {
		t.Fatalf("%+v %v", got, err)
	}
}

// The areas are the root categories and provinces with at least the
// minimum of active listings, with the latest change among them.
func TestSitemapAreas(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	hn, hcm := domain.Point{Lat: 21.03, Lng: 105.85}, domain.Point{Lat: 10.97, Lng: 106.66}
	var newest uuid.UUID
	for i := range 5 {
		id := put(t, db, item{title: "Xe đạp", cat: "vehicles.bicycle", price: 1_000_000, point: hn, age: time.Duration(i+1) * time.Hour})
		if i == 0 {
			newest = id
		}
		put(t, db, item{title: "Xe máy", cat: "vehicles.motorbike", price: 9_000_000, point: hcm, commune: "25747", age: time.Hour})
	}
	for range 4 {
		put(t, db, item{title: "iPhone", cat: "phones.iphone", price: 5_000_000, point: hn})
	}
	put(t, db, item{title: "iPhone", cat: "phones.iphone", price: 5_000_000, point: hn, status: domain.StatusReserved})
	var latest time.Time
	if err := db.QueryRow(ctx, `SELECT updated_at FROM listings WHERE id = $1`, newest).Scan(&latest); err != nil {
		t.Fatal(err)
	}
	got, err := repository.NewListings(db).SitemapAreas(ctx, 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if a := got[0]; a.CategoryRoot != "vehicles" || a.ProvinceCode != "01" || a.Count != 5 || !a.UpdatedAt.Equal(latest) {
		t.Fatalf("Hà Nội: %+v, want updated %v", a, latest)
	}
	if a := got[1]; a.CategoryRoot != "vehicles" || a.ProvinceCode != "79" || a.Count != 5 {
		t.Fatalf("TP Hồ Chí Minh: %+v", a)
	}
}
