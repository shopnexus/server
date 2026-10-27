package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

// Search answers cards in the index's order, with the commune's name and
// whole kilometres from the origin.
func TestSearchCards(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ba := domain.Point{Lat: 21.03, Lng: 105.82}
	put := func(title string, p domain.Point) uuid.UUID {
		t.Helper()
		at := time.Now()
		id := uuid.NewV7()
		l := domain.RestoreListing(domain.ListingState{
			ID: id, SellerID: e.account(t), Title: title, Price: 1_500_000, CategoryCode: "vehicles.bicycle",
			Condition: domain.ConditionUsed, CommuneCode: "00004", Photos: []domain.Photo{{UploadID: uuid.NewV7()}},
			Status: domain.StatusActive, WriteID: id, CreatedAt: at, UpdatedAt: at, PublishedAt: &at, Lat: p.Lat, Lng: p.Lng,
		})
		if err := e.listings.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
		return id
	}
	near := put("Xe đạp Asama", ba)
	far := put("Xe đạp Giant", domain.Point{Lat: 21.05, Lng: 105.82})
	cards, err := e.catalog.Search(ctx, domain.Search{
		Phrases: []domain.Phrase{{Text: "xe đạp", Weight: 1}}, Sort: domain.SortNearest, Limit: 20,
		Filters: domain.Filters{Origin: &ba},
	})
	if err != nil || len(cards) != 2 || cards[0].ID != near || cards[1].ID != far {
		t.Fatalf("%+v %v", cards, err)
	}
	if cards[0].DistanceKm == nil || *cards[0].DistanceKm != 0 || cards[1].DistanceKm == nil || *cards[1].DistanceKm != 2 ||
		cards[0].Commune == "" {
		t.Fatalf("distance and commune: %+v", cards)
	}
	plain, err := e.catalog.Search(ctx, domain.Search{Phrases: []domain.Phrase{{Text: "xe đạp", Weight: 1}}, Sort: domain.SortRelevance, Limit: 20})
	if err != nil || len(plain) != 2 || plain[0].DistanceKm != nil {
		t.Fatalf("no origin, no distance: %+v %v", plain, err)
	}
	if _, ok, err := e.catalog.Place(ctx, "Hà Nội"); err != nil || !ok {
		t.Fatalf("Hà Nội: %v %v", ok, err)
	}
	if _, ok, err := e.catalog.CommunePoint(ctx, "nope"); err != nil || ok {
		t.Fatalf("an unknown commune: %v %v", ok, err)
	}
	if p, ok, err := e.catalog.CommunePoint(ctx, "00004"); err != nil || !ok || p.Lat == 0 {
		t.Fatalf("Ba Đình: %v %v %v", p, ok, err)
	}
	counts, err := e.catalog.Counts(ctx, domain.Search{}, domain.CountRoot)
	if err != nil || len(counts) != 1 || counts[0].Count != 2 {
		t.Fatalf("counts: %v %v", counts, err)
	}
}
