package domain

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strings"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

// ForYouDays is how many UTC days, today included, Dành cho bạn and Vì bạn
// đã xem gần đây read a person's views, saves and searches from.
const ForYouDays = 30

// The weight one signal adds to its subcategory in Dành cho bạn: a save of
// a listing in it, a search whose first category chip names it, and a
// listing in it the person opened.
const (
	SaveWeight   = 3
	SearchWeight = 2
	ViewWeight   = 1
)

// MaxViewed is how many listings Vì bạn đã xem gần đây holds.
const MaxViewed = 10

// MaxViewSignals and MaxSaveSignals bound what Dành cho bạn reads of a
// person's window: the newest 100 listing marks and the newest 100 saves.
const (
	MaxViewSignals = 100
	MaxSaveSignals = 100
)

// ForYouSince is the start of the ForYouDays UTC days that end on now's day,
// the days views are stored by.
func ForYouSince(now time.Time) time.Time {
	return ViewDay(now).AddDate(0, 0, -(ForYouDays - 1))
}

// MaxForYouBoosts is how many subcategories Dành cho bạn boosts. Each one
// is a branch of the feed's query, so a person who looked at everything
// would make it slow.
const MaxForYouBoosts = 5

// ForYouBoosts are the boosts of Dành cho bạn, one per subcategory, sorted
// by code: each subcategory weighs SaveWeight for every code in saved,
// SearchWeight for every one in searched and ViewWeight for every one in
// opened. A code without a dot, a root or none, adds nothing. Only the
// MaxForYouBoosts heaviest are kept, a tie going to the lower code. A
// boost's weight is the square root of its points, so one interest read
// many times does not crowd out the others.
func ForYouBoosts(saved, searched, opened []string) []catalogdomain.Boost {
	points := map[string]int{}
	for _, signal := range []struct {
		codes  []string
		weight int
	}{{saved, SaveWeight}, {searched, SearchWeight}, {opened, ViewWeight}} {
		for _, code := range signal.codes {
			if strings.Contains(code, ".") {
				points[code] += signal.weight
			}
		}
	}
	codes := slices.SortedFunc(maps.Keys(points), func(a, b string) int {
		return cmp.Or(cmp.Compare(points[b], points[a]), cmp.Compare(a, b))
	})
	codes = codes[:min(len(codes), MaxForYouBoosts)]
	slices.Sort(codes)
	out := make([]catalogdomain.Boost, 0, len(codes))
	for _, code := range codes {
		out = append(out, catalogdomain.Boost{Kind: catalogdomain.BoostCategory, Code: code, Weight: math.Sqrt(float64(points[code]))})
	}
	return out
}

// ViewMark is a listing a person opened, the last UTC day they opened it,
// and the listing's subcategory.
type ViewMark struct {
	Listing  uuid.UUID
	Day      time.Time
	Category string
}

// ViewedCategory is the subcategory of Vì bạn đã xem gần đây: among the
// marks of the latest day in marks, the subcategory most of them have. A
// tie goes to the subcategory of the listing created last, the one with the
// highest ID, as a listing's ID is a UUIDv7 taken when it is created. False
// without a mark that has a subcategory.
func ViewedCategory(marks []ViewMark) (string, bool) {
	var latest time.Time
	for _, m := range marks {
		if m.Category != "" && m.Day.After(latest) {
			latest = m.Day
		}
	}
	count := map[string]int{}
	newest := map[string]uuid.UUID{}
	for _, m := range marks {
		if m.Category == "" || !m.Day.Equal(latest) {
			continue
		}
		count[m.Category]++
		if m.Listing.Compare(newest[m.Category]) > 0 {
			newest[m.Category] = m.Listing
		}
	}
	best := ""
	for code, n := range count {
		if best == "" || n > count[best] || (n == count[best] && newest[code].Compare(newest[best]) > 0) {
			best = code
		}
	}
	return best, best != ""
}

// Distinct keeps the first of the cards that share a seller and a title
// (by QueryKey): a seller who posted the same thing twice shows once.
func Distinct[T any](cards []T, key func(T) (uuid.UUID, string)) []T {
	seen := make(map[[2]string]bool, len(cards))
	out := cards[:0:0]
	for _, c := range cards {
		seller, title := key(c)
		k := [2]string{seller.String(), QueryKey(title)}
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out
}
