package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

func (s *Deals) Get(ctx context.Context, viewer, id uuid.UUID) (DealView, error) {
	d, _, err := s.own(ctx, viewer, id)
	if err != nil {
		return DealView{}, err
	}
	l, err := s.listing(ctx, d.ListingID())
	if err != nil {
		return DealView{}, err
	}
	return s.read(ctx, viewer, d, l)
}

// ListMine reads one page of the viewer's deals with their listings and
// moves in three queries.
func (s *Deals) ListMine(ctx context.Context, viewer uuid.UUID, as domain.Party, p page.Request) (page.Page[DealView], error) {
	rows, err := s.deals.ListByParty(ctx, viewer, as, p)
	if err != nil {
		return page.Page[DealView]{}, fmt.Errorf("list deals: %w", err)
	}
	listingIDs, dealIDs := make([]uuid.UUID, 0, len(rows)), make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		listingIDs, dealIDs = append(listingIDs, rows[i].ListingID()), append(dealIDs, rows[i].ID())
	}
	listings, err := s.catalog.Summaries(ctx, listingIDs)
	if err != nil {
		return page.Page[DealView]{}, fmt.Errorf("read listings: %w", err)
	}
	moves, err := s.deals.Moves(ctx, dealIDs)
	if err != nil {
		return page.Page[DealView]{}, fmt.Errorf("read moves: %w", err)
	}
	items := make([]DealView, 0, len(rows))
	for i := range rows {
		items = append(items, view(viewer, rows[i], listings[rows[i].ListingID()], moves[rows[i].ID()]))
	}
	return page.Of(items, p, func(v DealView) uuid.UUID { return v.ID }), nil
}
