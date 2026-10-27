package repository_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

type item struct {
	title  string
	cat    string
	price  int64
	point  domain.Point
	age    time.Duration
	status domain.Status
}

// put stores an item as a published listing of a fresh seller.
func put(t *testing.T, db *pgxpool.Pool, it item) uuid.UUID {
	t.Helper()
	status := it.status
	if status == "" {
		status = domain.StatusActive
	}
	at := time.Now().Add(-it.age)
	id := uuid.NewV7()
	l := domain.RestoreListing(domain.ListingState{
		ID: id, SellerID: newAccount(t, db), Title: it.title, Price: it.price, CategoryCode: it.cat,
		Condition: domain.ConditionUsed, AllowOffers: true, CommuneCode: "00004",
		Photos: []domain.Photo{{UploadID: uuid.NewV7()}}, Status: status, WriteID: id,
		CreatedAt: at, UpdatedAt: at, PublishedAt: &at, Lat: it.point.Lat, Lng: it.point.Lng,
	})
	if err := repository.NewListings(db).Insert(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSearch(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewSearches(db)
	hn := domain.Point{Lat: 21.03, Lng: 105.85}
	touring := put(t, db, item{title: "Xe đạp touring Giant", cat: "vehicles.bicycle", price: 2_500_000, point: hn, age: 24 * time.Hour})
	mtb := put(t, db, item{
		title: "Xe đạp địa hình", cat: "vehicles.bicycle", price: 1_800_000,
		point: domain.Point{Lat: 21.20, Lng: 105.85}, age: 20 * 24 * time.Hour,
	})
	lamp := put(t, db, item{title: "Đèn xe đạp", cat: "sports.other", price: 150_000, point: hn, age: time.Hour})
	put(t, db, item{title: "Xe đạp trẻ em", cat: "baby.toys", price: 900_000, point: hn, status: domain.StatusReserved})
	put(t, db, item{title: "iPhone 13", cat: "phones.iphone", price: 8_000_000, point: hn})

	ids := func(q domain.Search) []uuid.UUID {
		t.Helper()
		q.Limit = 20
		hits, err := s.Search(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]uuid.UUID, len(hits))
		for i, h := range hits {
			out[i] = h.ID
		}
		return out
	}
	text := []domain.Phrase{{Text: "xe đạp", Weight: 1}}

	if got := ids(domain.Search{Phrases: text, Sort: domain.SortRelevance}); len(got) != 3 {
		t.Fatalf("only matching, active listings: %v", got)
	}
	got := ids(domain.Search{Phrases: text, Sort: domain.SortRelevance, Boosts: []domain.Boost{
		{Kind: domain.BoostCategory, Code: "vehicles.bicycle", Weight: 1.5},
		{Kind: domain.BoostCategoryRoot, Code: "sports", Weight: -1.5},
	}})
	if len(got) != 3 || got[2] != lamp {
		t.Fatalf("a demote puts the accessory last: %v", got)
	}
	got = ids(domain.Search{
		Phrases: text, Sort: domain.SortRelevance, Filters: domain.Filters{CategoryCode: "vehicles.bicycle"},
		Boosts: []domain.Boost{{Kind: domain.BoostNear, Near: hn, RadiusKm: 5, Weight: 2}},
	})
	if len(got) != 2 || got[0] != touring {
		t.Fatalf("a near ring puts the Hà Nội bike first: %v", got)
	}
	if got = ids(domain.Search{
		Phrases: text, Sort: domain.SortRelevance, Filters: domain.Filters{PriceMax: 2_000_000, CategoryRoot: "vehicles"},
	}); len(got) != 1 || got[0] != mtb {
		t.Fatalf("a narrow filter keeps its one match: %v", got)
	}
	if got = ids(domain.Search{Sort: domain.SortPriceAsc, Filters: domain.Filters{Origin: &hn, RadiusKm: 10}}); len(got) != 3 || got[0] != lamp {
		t.Fatalf("a browse by price inside 10 km: %v", got)
	}
	if got = ids(domain.Search{Sort: domain.SortNearest, Filters: domain.Filters{Origin: &hn, RadiusKm: 50, CategoryCode: "vehicles.bicycle"}}); len(got) != 2 || got[1] != mtb {
		t.Fatalf("nearest first: %v", got)
	}
	if got = ids(domain.Search{
		Sort: domain.SortRelevance, Filters: domain.Filters{CategoryRoot: "vehicles"},
		Boosts: []domain.Boost{{Kind: domain.BoostFresh, Since: time.Now().Add(-3 * 24 * time.Hour), Weight: 1}},
	}); len(got) != 2 || got[0] != touring {
		t.Fatalf("a fresh ring orders a browse: %v", got)
	}
	counts, err := s.Counts(ctx, domain.Search{Phrases: text, Filters: domain.Filters{CategoryRoot: "vehicles"}}, domain.CountCode)
	if err != nil || len(counts) != 1 || counts[0].Code != "vehicles.bicycle" || counts[0].Count != 2 {
		t.Fatalf("counts under a root: %v %v", counts, err)
	}
	roots, err := s.Counts(ctx, domain.Search{}, domain.CountRoot)
	if err != nil || !slices.Contains(roots, domain.CategoryCount{Code: "vehicles", Count: 2}) {
		t.Fatalf("counts by root: %v %v", roots, err)
	}
	if got = ids(domain.Search{Phrases: []domain.Phrase{{Text: "?!", Weight: 1}}, Sort: domain.SortRelevance}); len(got) != 0 {
		t.Fatalf("punctuation matches nothing: %v", got)
	}
	if got = ids(domain.Search{Phrases: []domain.Phrase{{Text: "giabt", Weight: 1}}, Sort: domain.SortRelevance}); len(got) != 1 || got[0] != touring {
		t.Fatalf("a typo in a long word matches through fuzzy: %v", got)
	}
}
