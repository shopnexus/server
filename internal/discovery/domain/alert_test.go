package domain_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/discovery/domain"
)

// Level 5 tr over 6 → 4.8 → 4.5 → 5.5 → 4.9 tells at 4.8, 4.5 and 4.9.
func TestPriceStepTellsAtEachNewLow(t *testing.T) {
	t.Parallel()
	level := domain.AlertLevel(new(int64(5_000_000)), 6_000_000)
	if level != 5_000_000 {
		t.Fatalf("%d", level)
	}
	var alerted *int64
	var told []int64
	for _, price := range []int64{6_000_000, 4_800_000, 4_500_000, 5_500_000, 4_900_000, 4_900_000} {
		tell, reset := domain.PriceStep(level, alerted, price)
		switch {
		case tell:
			alerted = new(price)
			told = append(told, price)
		case reset:
			alerted = nil
		default:
		}
	}
	if len(told) != 3 || told[0] != 4_800_000 || told[1] != 4_500_000 || told[2] != 4_900_000 {
		t.Fatalf("%v", told)
	}
}

func TestAlertLevel(t *testing.T) {
	t.Parallel()
	if l := domain.AlertLevel(nil, 2_000_000); l != 2_000_000 {
		t.Fatalf("no level of its own: %d", l)
	}
	if l := domain.AlertLevel(new(int64(1_000_000)), 2_000_000); l != 1_000_000 {
		t.Fatalf("own level wins: %d", l)
	}
	if tell, reset := domain.PriceStep(5, new(int64(1)), 5); tell || !reset {
		t.Fatal("a price back at the level clears what was told")
	}
}
