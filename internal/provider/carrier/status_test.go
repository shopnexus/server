package carrier_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/provider/carrier"
)

func TestGHTKStatuses(t *testing.T) {
	t.Parallel()
	for id, want := range map[int]carrier.Status{
		3: carrier.StatusInTransit, 4: carrier.StatusInTransit, 5: carrier.StatusDelivered, 6: carrier.StatusDelivered,
		9: carrier.StatusFailed, 20: carrier.StatusFailed, 21: carrier.StatusFailed,
	} {
		if got, ok := carrier.GHTKStatus(id); !ok || got != want {
			t.Errorf("%d: %s %v", id, got, ok)
		}
	}
	for _, id := range []int{-1, 1, 2, 7, 8, 10, 12, 123, 127, 128, 45, 49, 410} {
		if got, ok := carrier.GHTKStatus(id); ok {
			t.Errorf("%d moves the shipment to %s", id, got)
		}
	}
}

func TestGHNStatuses(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]carrier.Status{
		"picked": carrier.StatusInTransit, "storing": carrier.StatusInTransit, "transporting": carrier.StatusInTransit,
		"sorting": carrier.StatusInTransit, "delivering": carrier.StatusInTransit, "money_collect_delivering": carrier.StatusInTransit,
		"delivered":     carrier.StatusDelivered,
		"delivery_fail": carrier.StatusFailed, "waiting_to_return": carrier.StatusFailed, "return": carrier.StatusFailed,
		"return_transporting": carrier.StatusFailed, "return_sorting": carrier.StatusFailed, "returning": carrier.StatusFailed,
		"returned": carrier.StatusFailed,
	} {
		if got, ok := carrier.GHNStatus(s); !ok || got != want {
			t.Errorf("%s: %s %v", s, got, ok)
		}
	}
	for _, s := range []string{"ready_to_pick", "picking", "cancel", "exception", "lost", "damage", ""} {
		if got, ok := carrier.GHNStatus(s); ok {
			t.Errorf("%q moves the shipment to %s", s, got)
		}
	}
}
