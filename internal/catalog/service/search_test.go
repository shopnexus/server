package service_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/service"
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
		cards[0].Commune == "" || cards[0].SellerID == uuid.Nil() || cards[0].CategoryCode != "vehicles.bicycle" {
		t.Fatalf("distance, commune, seller and category: %+v", cards)
	}
	plain, err := e.catalog.Search(ctx, domain.Search{Phrases: []domain.Phrase{{Text: "xe đạp", Weight: 1}}, Sort: domain.SortRelevance, Limit: 20})
	if err != nil || len(plain) != 2 || plain[0].DistanceKm != nil {
		t.Fatalf("no origin, no distance: %+v %v", plain, err)
	}
	if places, placeErr := e.catalog.FindPlaces(ctx, []string{"Hà Nội", "Atlantis"}); placeErr != nil || len(places) != 1 || places["Hà Nội"].Lat == 0 {
		t.Fatalf("Hà Nội: %v %v", places, placeErr)
	}
	counts, err := e.catalog.Counts(ctx, domain.Search{})
	if err != nil || len(counts) != 1 || counts[0].Count != 2 {
		t.Fatalf("counts: %v %v", counts, err)
	}
}

// A card carries the thumbnails of the listing's first photos in order, the
// cover first, and none of a listing whose photos are not processed.
func TestCardPhotoThumbs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	at := time.Now()
	put := func(title string, photos []domain.Photo) uuid.UUID {
		t.Helper()
		id := uuid.NewV7()
		l := domain.RestoreListing(domain.ListingState{
			ID: id, SellerID: e.account(t), Title: title, Price: 1_500_000, CategoryCode: "vehicles.bicycle",
			Condition: domain.ConditionUsed, CommuneCode: "00004", Photos: photos,
			Status: domain.StatusActive, WriteID: id, CreatedAt: at, UpdatedAt: at, PublishedAt: &at,
		})
		if err := e.listings.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
		return id
	}
	photos := make([]domain.Photo, service.CardPhotos+2)
	for i := range photos {
		key := fmt.Sprintf("media/p%d", i)
		photos[i] = domain.Photo{UploadID: uuid.NewV7(), Keys: domain.PhotoKeys{Photo: key + ".jpg", Thumb: key + "_thumb.jpg"}}
	}
	many := put("Xe đạp Asama", photos)
	none := put("Xe đạp Giant", []domain.Photo{{UploadID: uuid.NewV7()}})
	cards, err := e.catalog.Search(ctx, domain.Search{Phrases: []domain.Phrase{{Text: "xe đạp", Weight: 1}}, Sort: domain.SortRelevance, Limit: 20})
	if err != nil || len(cards) != 2 {
		t.Fatalf("%+v %v", cards, err)
	}
	byID := map[uuid.UUID]service.SearchCard{cards[0].ID: cards[0], cards[1].ID: cards[1]}
	c := byID[many]
	if len(c.PhotoThumbURLs) != service.CardPhotos || c.PhotoThumbURLs[0] != c.CoverThumbURL {
		t.Fatalf("thumbs: %v, cover %s", c.PhotoThumbURLs, c.CoverThumbURL)
	}
	for i, u := range c.PhotoThumbURLs {
		if !strings.HasSuffix(u, fmt.Sprintf("/media/p%d_thumb.jpg", i)) {
			t.Fatalf("thumb %d: %s", i, u)
		}
	}
	if c := byID[none]; c.PhotoThumbURLs != nil || c.CoverThumbURL != "" {
		t.Fatalf("an unprocessed photo has no thumb: %+v", c)
	}
}
