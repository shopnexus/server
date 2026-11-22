package domain_test

import (
	"slices"
	"testing"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
)

// The window is the 30 UTC days that end today.
func TestForYouSince(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 17, 23, 30, 0, 0, time.UTC)
	if got := domain.ForYouSince(now); !got.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("%v", got)
	}
}

// A save weighs 3, a search 2 and a view 1 in its subcategory; a root or an
// empty code weighs nothing, and the boosts come sorted by code.
func TestForYouBoosts(t *testing.T) {
	t.Parallel()
	got := domain.ForYouBoosts(
		[]string{"baby.toys"},
		[]string{"home.washer", "vehicles", ""},
		[]string{"home.washer", "baby.toys", "sports.gym", "sports.gym"},
	)
	want := []catalogdomain.Boost{
		{Kind: catalogdomain.BoostCategory, Code: "baby.toys", Weight: 4},
		{Kind: catalogdomain.BoostCategory, Code: "home.washer", Weight: 3},
		{Kind: catalogdomain.BoostCategory, Code: "sports.gym", Weight: 2},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
	if got = domain.ForYouBoosts(nil, nil, nil); len(got) != 0 {
		t.Fatalf("no signals: %+v", got)
	}
}

// The rail takes the subcategory most opened on the latest day; a tie goes
// to the subcategory of the listing created last.
func TestViewedCategory(t *testing.T) {
	t.Parallel()
	today := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	// UUIDv7s taken in this order sort in it.
	first, second, third, fourth := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	for name, c := range map[string]struct {
		marks []domain.ViewMark
		want  string
	}{
		"the most opened": {[]domain.ViewMark{
			{Listing: first, Day: today, Category: "sports.gym"},
			{Listing: second, Day: today, Category: "sports.gym"},
			{Listing: fourth, Day: today, Category: "baby.toys"},
			{Listing: third, Day: yesterday, Category: "baby.toys"},
		}, "sports.gym"},
		"a tie": {[]domain.ViewMark{
			{Listing: fourth, Day: today, Category: "sports.gym"},
			{Listing: second, Day: today, Category: "home.fan"},
			{Listing: first, Day: today, Category: "sports.gym"},
			{Listing: third, Day: today, Category: "home.fan"},
			{Listing: uuid.NewV7(), Day: yesterday, Category: "home.fan"},
		}, "sports.gym"},
		"a tie of one each": {[]domain.ViewMark{
			{Listing: second, Day: today, Category: "sports.gym"},
			{Listing: third, Day: today, Category: "baby.toys"},
		}, "baby.toys"},
		"only an older day": {[]domain.ViewMark{
			{Listing: first, Day: yesterday, Category: "sports.gym"},
			{Listing: second, Day: today},
		}, "sports.gym"},
	} {
		if got, ok := domain.ViewedCategory(c.marks); !ok || got != c.want {
			t.Errorf("%s: %q %v", name, got, ok)
		}
	}
	if got, ok := domain.ViewedCategory(nil); ok || got != "" {
		t.Fatalf("no marks: %q", got)
	}
}

func TestDistinct(t *testing.T) {
	t.Parallel()
	a, b := uuid.NewV7(), uuid.NewV7()
	type card struct {
		seller uuid.UUID
		title  string
	}
	got := domain.Distinct([]card{{a, "iPhone 13"}, {a, " iphone  13"}, {b, "iPhone 13"}, {a, "iPhone 14"}},
		func(c card) (uuid.UUID, string) { return c.seller, c.title })
	if len(got) != 3 || got[1].seller != b || got[2].title != "iPhone 14" {
		t.Fatalf("%+v", got)
	}
}
