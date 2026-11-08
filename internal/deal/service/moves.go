package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
)

// MoveFacts is what a deal's new moves tell: who, on which listing, and the
// deal's latest order.
type MoveFacts struct {
	DealID, BuyerID, SellerID           uuid.UUID
	ListingTitle, BuyerName, SellerName string
	Moves                               []domain.Move
	Order                               *domain.Order
}

func (s *Deals) Moves(ctx context.Context, dealID uuid.UUID, ids []uuid.UUID) (MoveFacts, bool, error) {
	d, err := s.deals.Get(ctx, dealID)
	if errors.Is(err, repository.ErrNotFound) {
		return MoveFacts{}, false, nil
	}
	if err != nil {
		return MoveFacts{}, false, fmt.Errorf("read deal: %w", err)
	}
	st := d.State()
	moves, err := s.deals.Moves(ctx, []uuid.UUID{dealID})
	if err != nil {
		return MoveFacts{}, false, fmt.Errorf("read moves: %w", err)
	}
	people, err := s.accounts.GetMany(ctx, []uuid.UUID{st.BuyerID, st.SellerID})
	if err != nil {
		return MoveFacts{}, false, fmt.Errorf("read people: %w", err)
	}
	listing, _, err := s.catalog.Summary(ctx, st.ListingID)
	if err != nil {
		return MoveFacts{}, false, fmt.Errorf("read listing: %w", err)
	}
	f := MoveFacts{
		DealID: dealID, BuyerID: st.BuyerID, SellerID: st.SellerID,
		ListingTitle: listing.Title, BuyerName: people[st.BuyerID].DisplayName,
		SellerName: people[st.SellerID].DisplayName, Order: d.Order(),
	}
	for _, m := range moves[dealID] {
		if slices.Contains(ids, m.ID) {
			f.Moves = append(f.Moves, m)
		}
	}
	return f, true, nil
}
