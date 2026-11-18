package domain_test

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
)

func listing(t *testing.T, change func(*domain.ListingInput)) domain.Listing {
	t.Helper()
	photo := uuid.NewV7()
	d := domain.ListingInput{
		Title: "iPhone 13 128GB", Price: 9_500_000, Description: "Máy zin, pin 89%", CategoryCode: "phones.iphone",
		Condition: domain.ConditionLikeNew, CommuneCode: "00004", Photos: []uuid.UUID{photo},
		Specs: []domain.Spec{{Label: "Màu", Value: "Xanh"}},
	}
	if change != nil {
		change(&d)
	}
	l, err := domain.NewListing(uuid.NewV7(), uuid.NewV7(), d, domain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	l.Published(map[uuid.UUID]domain.PhotoKeys{photo: {Photo: "media/p.jpg", Thumb: "media/p_thumb.jpg"}}, time.Now())
	return l
}

func TestEmbeddingText(t *testing.T) {
	t.Parallel()
	l := listing(t, nil)
	got := l.EmbeddingText("iPhone")
	want := "iPhone 13 128GB\niPhone\nNhư mới\nMàu: Xanh\nMáy zin, pin 89%"
	if got != want {
		t.Fatalf("%q", got)
	}
	long := listing(t, func(d *domain.ListingInput) {
		d.Description = strings.Repeat("ạ", 1500)
		d.Specs = []domain.Spec{{Label: strings.Repeat("x", 20), Value: strings.Repeat("y", 50)}}
	})
	if n := len([]rune(long.EmbeddingText(strings.Repeat("c", 600)))); n != domain.EmbeddingTextMax {
		t.Fatalf("%d runes", n)
	}
}

func TestTextHash(t *testing.T) {
	t.Parallel()
	a, b := domain.TextHash("v1", "x"), domain.TextHash("v2", "x")
	if bytes.Equal(a, b) || !bytes.Equal(a, domain.TextHash("v1", "x")) || len(a) != 32 {
		t.Fatal("the hash must change with the version and only with it")
	}
}

func TestProcessedPhotos(t *testing.T) {
	t.Parallel()
	l := listing(t, nil)
	if got := l.ProcessedPhotos(); len(got) != 1 || got[0] != "media/p.jpg" {
		t.Fatal(got)
	}
}

func TestHybrid(t *testing.T) {
	t.Parallel()
	if (domain.Search{}).Hybrid() || !(domain.Search{Weights: domain.Weights{Photo: 1}}).Hybrid() {
		t.Fatal("Hybrid follows the weights")
	}
}

// A draft embeds to the text the listing posted from it embeds to, with the
// same trimming.
func TestDraftEmbeddingText(t *testing.T) {
	t.Parallel()
	l := listing(t, nil)
	d := domain.ListingInput{
		Title: "  iPhone 13 128GB ", Description: " Máy zin, pin 89% ", Condition: domain.ConditionLikeNew,
		Specs: []domain.Spec{{Label: " Màu ", Value: "Xanh "}},
	}
	if got, want := d.EmbeddingText("iPhone"), l.EmbeddingText("iPhone"); got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	if got := (domain.ListingInput{Title: "Xe đạp", Condition: domain.ConditionUsed}).EmbeddingText("Xe đạp"); got != "Xe đạp\nXe đạp\nĐã dùng" {
		t.Fatalf("no specs, no description: %q", got)
	}
}
