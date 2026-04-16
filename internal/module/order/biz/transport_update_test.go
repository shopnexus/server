package orderbiz_test

import (
	"testing"

	orderbiz "shopnexus-server/internal/module/order/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
)

// TestTransportStatusTransitions verifies the state machine rules defined in
// validTransitions. Happy path: Pending → LabelCreated → InTransit →
// OutForDelivery → Delivered. Any state may move to Failed/Cancelled (terminal).
// Terminal states (Delivered, Failed, Cancelled) may not transition further.
func TestTransportStatusTransitions(t *testing.T) {
	cases := []struct {
		name      string
		from      orderdb.OrderTransportStatus
		to        orderdb.OrderTransportStatus
		wantValid bool
	}{
		// Happy path
		{"Pending→LabelCreated", orderdb.OrderTransportStatusPending, orderdb.OrderTransportStatusLabelCreated, true},
		{
			"LabelCreated→InTransit",
			orderdb.OrderTransportStatusLabelCreated,
			orderdb.OrderTransportStatusInTransit,
			true,
		},
		{
			"InTransit→OutForDelivery",
			orderdb.OrderTransportStatusInTransit,
			orderdb.OrderTransportStatusOutForDelivery,
			true,
		},
		{
			"OutForDelivery→Delivered",
			orderdb.OrderTransportStatusOutForDelivery,
			orderdb.OrderTransportStatusDelivered,
			true,
		},

		// Exception path from any active state
		{"Pending→Failed", orderdb.OrderTransportStatusPending, orderdb.OrderTransportStatusFailed, true},
		{
			"LabelCreated→Cancelled",
			orderdb.OrderTransportStatusLabelCreated,
			orderdb.OrderTransportStatusCancelled,
			true,
		},
		{"InTransit→Failed", orderdb.OrderTransportStatusInTransit, orderdb.OrderTransportStatusFailed, true},

		// Skip-ahead invalid
		{"Pending→InTransit (skip)", orderdb.OrderTransportStatusPending, orderdb.OrderTransportStatusInTransit, false},
		{
			"LabelCreated→OutForDelivery (skip)",
			orderdb.OrderTransportStatusLabelCreated,
			orderdb.OrderTransportStatusOutForDelivery,
			false,
		},
		{
			"InTransit→Delivered (skip)",
			orderdb.OrderTransportStatusInTransit,
			orderdb.OrderTransportStatusDelivered,
			false,
		},

		// Backward invalid
		{
			"InTransit→LabelCreated (back)",
			orderdb.OrderTransportStatusInTransit,
			orderdb.OrderTransportStatusLabelCreated,
			false,
		},
		{
			"Delivered→InTransit (back)",
			orderdb.OrderTransportStatusDelivered,
			orderdb.OrderTransportStatusInTransit,
			false,
		},

		// Terminal states cannot transition
		{
			"Delivered→Cancelled (terminal)",
			orderdb.OrderTransportStatusDelivered,
			orderdb.OrderTransportStatusCancelled,
			false,
		},
		{
			"Failed→InTransit (terminal)",
			orderdb.OrderTransportStatusFailed,
			orderdb.OrderTransportStatusInTransit,
			false,
		},
		{
			"Cancelled→Delivered (terminal)",
			orderdb.OrderTransportStatusCancelled,
			orderdb.OrderTransportStatusDelivered,
			false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed := orderbiz.ValidTransitions[tc.from]
			got := allowed[tc.to]
			if got != tc.wantValid {
				t.Fatalf("transition %s→%s: expected valid=%v, got %v",
					tc.from, tc.to, tc.wantValid, got)
			}
		})
	}
}
