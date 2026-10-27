package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
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

// ListMine reads one page of the viewer's deals.
func (s *Deals) ListMine(ctx context.Context, viewer uuid.UUID, as domain.Party, p page.Request) (page.Page[DealView], error) {
	rows, err := s.deals.ListByParty(ctx, viewer, as, p)
	if err != nil {
		return page.Page[DealView]{}, fmt.Errorf("list deals: %w", err)
	}
	items, err := s.views(ctx, viewer, rows)
	if err != nil {
		return page.Page[DealView]{}, err
	}
	return page.Of(items, p, func(v DealView) uuid.UUID { return v.ID }), nil
}

func (s *Deals) Between(ctx context.Context, viewer, other uuid.UUID) ([]DealView, error) {
	rows, err := s.deals.Between(ctx, viewer, other)
	if err != nil {
		return nil, fmt.Errorf("list deals: %w", err)
	}
	return s.views(ctx, viewer, rows)
}

// views builds the views of deals for viewer, reading their listings and
// moves in two queries and each order's place.
func (s *Deals) views(ctx context.Context, viewer uuid.UUID, deals []domain.Deal) ([]DealView, error) {
	listingIDs, dealIDs := make([]uuid.UUID, 0, len(deals)), make([]uuid.UUID, 0, len(deals))
	for i := range deals {
		listingIDs, dealIDs = append(listingIDs, deals[i].ListingID()), append(dealIDs, deals[i].ID())
	}
	listings, err := s.catalog.Summaries(ctx, listingIDs)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	moves, err := s.deals.Moves(ctx, dealIDs)
	if err != nil {
		return nil, fmt.Errorf("read moves: %w", err)
	}
	items := make([]DealView, 0, len(deals))
	for i := range deals {
		place, placeErr := s.placeOf(ctx, deals[i].Order())
		if placeErr != nil {
			return nil, placeErr
		}
		items = append(items, view(viewer, deals[i], listings[deals[i].ListingID()], moves[deals[i].ID()], place))
	}
	return items, nil
}

// ListOrders reads one page of the viewer's orders with their listings.
func (s *Deals) ListOrders(ctx context.Context, viewer uuid.UUID, as domain.Party, p page.Request) (page.Page[OrderItem], error) {
	rows, err := s.deals.ListOrders(ctx, viewer, as, p)
	if err != nil {
		return page.Page[OrderItem]{}, fmt.Errorf("list orders: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ListingID)
	}
	listings, err := s.catalog.Summaries(ctx, ids)
	if err != nil {
		return page.Page[OrderItem]{}, fmt.Errorf("read listings: %w", err)
	}
	items := make([]OrderItem, 0, len(rows))
	for i := range rows {
		place, placeErr := s.placeOf(ctx, &rows[i].Order)
		if placeErr != nil {
			return page.Page[OrderItem]{}, placeErr
		}
		items = append(items, OrderItem{
			OrderView: orderView(rows[i].Order, as, place), DealID: rows[i].DealID,
			Listing: listingRef(listings[rows[i].ListingID]),
		})
	}
	return page.Of(items, p, func(o OrderItem) uuid.UUID { return o.ID }), nil
}

func (s *Deals) Order(ctx context.Context, id uuid.UUID) (OrderFacts, error) {
	r, err := s.deals.Order(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return OrderFacts{}, domain.ErrOrderNotFound
	}
	if err != nil {
		return OrderFacts{}, fmt.Errorf("get order: %w", err)
	}
	return OrderFacts{
		ID: r.Order.ID, DealID: r.DealID, BuyerID: r.BuyerID, SellerID: r.SellerID, ListingID: r.ListingID,
		Status: r.Order.Status,
	}, nil
}
