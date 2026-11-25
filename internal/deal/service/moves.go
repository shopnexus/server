package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
)

// MoveFacts is what a deal's new moves tell: who, on which listing, the
// deal's latest order, and the orders the moves are steps of. The deal may
// have a newer order than a move's by now; a move tells of its own order,
// and a move that names none of the latest.
type MoveFacts struct {
	DealID, BuyerID, SellerID           uuid.UUID
	ListingTitle, BuyerName, SellerName string
	// ListingPhoto is the listing's cover thumbnail URL.
	ListingPhoto string
	Moves        []domain.Move
	// Order is the deal's latest order, nil before the first.
	Order *domain.Order
	// Orders holds the latest order and every order a move names, by ID.
	Orders map[uuid.UUID]domain.Order
}

func (s *Deals) Moves(ctx context.Context, dealID uuid.UUID, ids []uuid.UUID) (MoveFacts, bool, error) {
	d, err := s.d.Deals.Get(ctx, dealID)
	if errors.Is(err, repository.ErrNotFound) {
		return MoveFacts{}, false, nil
	}
	if err != nil {
		return MoveFacts{}, false, fmt.Errorf("read deal: %w", err)
	}
	st := d.State()
	moves, err := s.d.Deals.MovesOf(ctx, dealID, ids)
	if err != nil {
		return MoveFacts{}, false, fmt.Errorf("read moves: %w", err)
	}
	people, err := s.d.Accounts.GetMany(ctx, []uuid.UUID{st.BuyerID, st.SellerID})
	if err != nil {
		return MoveFacts{}, false, fmt.Errorf("read people: %w", err)
	}
	listing, _, err := s.d.Catalog.Summary(ctx, st.ListingID)
	if err != nil {
		return MoveFacts{}, false, fmt.Errorf("read listing: %w", err)
	}
	orders := map[uuid.UUID]domain.Order{}
	if o := d.Order(); o != nil {
		orders[o.ID] = *o
	}
	for _, m := range moves {
		if m.OrderID == nil {
			continue
		}
		if _, read := orders[*m.OrderID]; read {
			continue
		}
		r, orderErr := s.d.Deals.Order(ctx, *m.OrderID)
		if orderErr != nil {
			return MoveFacts{}, false, fmt.Errorf("read order %s: %w", *m.OrderID, orderErr)
		}
		orders[*m.OrderID] = r.Order
	}
	f := MoveFacts{
		DealID: dealID, BuyerID: st.BuyerID, SellerID: st.SellerID,
		ListingTitle: listing.Title, ListingPhoto: listing.CoverThumbURL, BuyerName: people[st.BuyerID].DisplayName,
		SellerName: people[st.SellerID].DisplayName, Moves: moves, Order: d.Order(), Orders: orders,
	}
	return f, true, nil
}
