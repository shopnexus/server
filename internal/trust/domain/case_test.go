package domain_test

import (
	"slices"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
)

// The most reported listing comes first; a tie goes to the later report.
func TestRankListings(t *testing.T) {
	t.Parallel()
	early, late, later := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	a, b, c := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	got := domain.RankListings([]domain.ListingTally{
		{ListingID: a, Reports: 1, LatestID: later},
		{ListingID: b, Reports: 2, LatestID: early},
		{ListingID: c, Reports: 2, LatestID: late},
	})
	if !slices.Equal(got, []uuid.UUID{c, b, a}) {
		t.Fatalf("got %v, want %v", got, []uuid.UUID{c, b, a})
	}
	if got := domain.RankListings(nil); len(got) != 0 {
		t.Fatalf("none: %v", got)
	}
}
