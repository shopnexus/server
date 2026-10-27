package service

import (
	"context"
	"fmt"
	"uuid"
)

// PairSummaries reads the pairs' candidate deals in one query and their
// listings in another.
func (s *Deals) PairSummaries(ctx context.Context, viewer uuid.UUID, others []uuid.UUID) (map[uuid.UUID]PairSummary, error) {
	deals, err := s.deals.OfPairs(ctx, viewer, others)
	if err != nil {
		return nil, fmt.Errorf("list deals: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(deals))
	for i := range deals {
		ids = append(ids, deals[i].ListingID())
	}
	listings, err := s.catalog.Summaries(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	// otherOf is the other side of each deal; active counts each pair's
	// active deals.
	otherOf := make([]uuid.UUID, len(deals))
	active := map[uuid.UUID]int{}
	for i := range deals {
		otherOf[i] = deals[i].SellerID()
		if otherOf[i] == viewer {
			otherOf[i] = deals[i].BuyerID()
		}
		if deals[i].Active() {
			active[otherOf[i]]++
		}
	}
	// The deals come newest first, so a pair shows its first deal of the
	// highest rank: 2 the viewer's turn, 1 active, 0 any other.
	out := map[uuid.UUID]PairSummary{}
	rank := map[uuid.UUID]int{}
	for i := range deals {
		d, other := &deals[i], otherOf[i]
		v := summary(viewer, d, listings[d.ListingID()])
		r := 0
		if d.Active() {
			r = 1
		}
		if v.YourTurn {
			r = 2
		}
		if best, seen := rank[other]; seen && best >= r {
			continue
		}
		rank[other] = r
		otherActive := active[other]
		if d.Active() {
			otherActive--
		}
		out[other] = PairSummary{Deal: v, OtherActive: otherActive}
	}
	return out, nil
}
