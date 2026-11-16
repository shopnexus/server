package domain_test

import (
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/discovery/domain"
)

// A view counts on its UTC day whatever the clock's zone, and a week is
// the seven UTC days that end today.
func TestViewDays(t *testing.T) {
	t.Parallel()
	ict := time.FixedZone("ICT", 7*60*60)
	late := time.Date(2026, 11, 16, 6, 30, 0, 0, ict) // 23:30 UTC on the 15th
	if got, want := domain.ViewDay(late), time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("day of %s: %s, want %s", late, got, want)
	}
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	if got, want := domain.ViewWeek(now), time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("the week of %s starts %s, want %s", now, got, want)
	}
}
