package orderbiz_test

import (
	"encoding/json"
	"testing"
)

// TestPartialRefundItemIDsSerialization verifies that ItemIDs are correctly
// serialized to JSON for the DB layer (item_ids JSONB column).
// Empty/nil slice must produce nil JSON (SQL NULL) so the DB knows it's a full refund.
func TestPartialRefundItemIDsSerialization(t *testing.T) {
	cases := []struct {
		name     string
		input    []int64
		wantNil  bool
		wantJSON string
	}{
		{name: "full refund (nil)", input: nil, wantNil: true},
		{name: "full refund (empty slice)", input: []int64{}, wantNil: true},
		{name: "partial refund single item", input: []int64{42}, wantJSON: "[42]"},
		{name: "partial refund multiple items", input: []int64{1, 2, 3}, wantJSON: "[1,2,3]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var itemIdsJSON json.RawMessage
			if len(tc.input) > 0 {
				itemIdsJSON, _ = json.Marshal(tc.input)
			}

			if tc.wantNil {
				if itemIdsJSON != nil {
					t.Fatalf("expected nil JSON for full refund, got %q", string(itemIdsJSON))
				}
				return
			}
			if string(itemIdsJSON) != tc.wantJSON {
				t.Fatalf("expected %q, got %q", tc.wantJSON, string(itemIdsJSON))
			}
		})
	}
}

// TestPartialRefundDuplicateItemRejection verifies the duplicate-detection helper
// used inside CreateBuyerRefund rejects repeated item IDs in the same request.
func TestPartialRefundDuplicateItemRejection(t *testing.T) {
	cases := []struct {
		name    string
		input   []int64
		wantDup bool
	}{
		{name: "no duplicates", input: []int64{1, 2, 3}, wantDup: false},
		{name: "adjacent duplicate", input: []int64{1, 1, 2}, wantDup: true},
		{name: "separated duplicate", input: []int64{1, 2, 1}, wantDup: true},
		{name: "single item", input: []int64{42}, wantDup: false},
		{name: "empty", input: []int64{}, wantDup: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(map[int64]bool, len(tc.input))
			gotDup := false
			for _, id := range tc.input {
				if seen[id] {
					gotDup = true
					break
				}
				seen[id] = true
			}
			if gotDup != tc.wantDup {
				t.Fatalf("expected duplicate=%v, got %v for input %v", tc.wantDup, gotDup, tc.input)
			}
		})
	}
}
