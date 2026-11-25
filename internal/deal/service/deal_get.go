package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/page"
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

func (s *Deals) BuyerSummary(ctx context.Context, buyer, listingID uuid.UUID) (DealSummary, bool, error) {
	d, err := s.d.Deals.Find(ctx, listingID, buyer)
	if errors.Is(err, repository.ErrNotFound) {
		return DealSummary{}, false, nil
	}
	if err != nil {
		return DealSummary{}, false, fmt.Errorf("find deal: %w", err)
	}
	l, err := s.listing(ctx, listingID)
	if err != nil {
		return DealSummary{}, false, err
	}
	return summary(buyer, &d, l), true, nil
}

// ListMine reads one page of the viewer's deals.
func (s *Deals) ListMine(ctx context.Context, viewer uuid.UUID, as domain.Party, p page.Request) (page.Page[DealView], error) {
	rows, err := s.d.Deals.ListByParty(ctx, viewer, as, p)
	if err != nil {
		return page.Page[DealView]{}, fmt.Errorf("list deals: %w", err)
	}
	kept := page.Of(rows, p, func(d domain.Deal) uuid.UUID { return d.ID() })
	items, err := s.views(ctx, viewer, kept.Items)
	if err != nil {
		return page.Page[DealView]{}, err
	}
	return page.Page[DealView]{Items: items, NextCursor: kept.NextCursor}, nil
}

// OpenOffers reads one page of the buyers' open offers on seller's
// listings. The page and its cursor come from the stored rows, so an offer
// its listing voided drops out without moving the cursor.
func (s *Deals) OpenOffers(ctx context.Context, seller uuid.UUID, p page.Request) (page.Page[DealView], error) {
	rows, err := s.d.Deals.OpenOffers(ctx, seller, p)
	if err != nil {
		return page.Page[DealView]{}, fmt.Errorf("list open offers: %w", err)
	}
	kept := page.Of(rows, p, func(d domain.Deal) uuid.UUID { return d.ID() })
	views, err := s.views(ctx, seller, kept.Items)
	if err != nil {
		return page.Page[DealView]{}, err
	}
	items := make([]DealView, 0, len(views))
	for i := range views {
		if o := views[i].OpenPrice; o != nil && o.By == domain.Buyer {
			items = append(items, views[i])
		}
	}
	return page.Page[DealView]{Items: items, NextCursor: kept.NextCursor}, nil
}

// OpenOfferCount counts the buyers' open offers on seller's listings.
func (s *Deals) OpenOfferCount(ctx context.Context, seller uuid.UUID) (int, error) {
	n, err := s.d.Deals.CountOpenOffers(ctx, seller)
	if err != nil {
		return 0, fmt.Errorf("count open offers: %w", err)
	}
	return n, nil
}

func (s *Deals) Between(ctx context.Context, viewer, other uuid.UUID) ([]DealView, error) {
	rows, err := s.d.Deals.Between(ctx, viewer, other)
	if err != nil {
		return nil, fmt.Errorf("list deals: %w", err)
	}
	return s.views(ctx, viewer, rows)
}

// views builds the views of deals for viewer, reading their listings, their
// moves and their orders' places in three queries.
func (s *Deals) views(ctx context.Context, viewer uuid.UUID, deals []domain.Deal) ([]DealView, error) {
	listingIDs, dealIDs := make([]uuid.UUID, 0, len(deals)), make([]uuid.UUID, 0, len(deals))
	var codes []string
	for i := range deals {
		listingIDs, dealIDs = append(listingIDs, deals[i].ListingID()), append(dealIDs, deals[i].ID())
		if o := deals[i].Order(); o != nil {
			codes = append(codes, o.Recipient.CommuneCode)
		}
	}
	listings, err := s.d.Catalog.Summaries(ctx, listingIDs)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	moves, err := s.d.Deals.Moves(ctx, dealIDs)
	if err != nil {
		return nil, fmt.Errorf("read moves: %w", err)
	}
	places, err := s.d.Catalog.Places(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("read the recipients' communes: %w", err)
	}
	items := make([]DealView, 0, len(deals))
	for i := range deals {
		var place catalogsvc.Location
		if o := deals[i].Order(); o != nil {
			place = places[o.Recipient.CommuneCode]
		}
		items = append(items, view(viewer, deals[i], listings[deals[i].ListingID()], moves[deals[i].ID()], place, s.d.Waits))
	}
	return items, nil
}

// ListOrders reads one page of the viewer's orders with their listings and
// places. The look-ahead row only sets the cursor; its listing and commune
// are not read.
func (s *Deals) ListOrders(ctx context.Context, viewer uuid.UUID, as domain.Party, p page.Request) (page.Page[OrderItem], error) {
	rows, err := s.d.Deals.ListOrders(ctx, viewer, as, p)
	if err != nil {
		return page.Page[OrderItem]{}, fmt.Errorf("list orders: %w", err)
	}
	kept := page.Of(rows, p, func(r repository.OrderRow) uuid.UUID { return r.Order.ID })
	ids, codes := make([]uuid.UUID, 0, len(kept.Items)), make([]string, 0, len(kept.Items))
	for i := range kept.Items {
		ids, codes = append(ids, kept.Items[i].ListingID), append(codes, kept.Items[i].Order.Recipient.CommuneCode)
	}
	listings, err := s.d.Catalog.Summaries(ctx, ids)
	if err != nil {
		return page.Page[OrderItem]{}, fmt.Errorf("read listings: %w", err)
	}
	places, err := s.d.Catalog.Places(ctx, codes)
	if err != nil {
		return page.Page[OrderItem]{}, fmt.Errorf("read the recipients' communes: %w", err)
	}
	items := make([]OrderItem, 0, len(kept.Items))
	for i := range kept.Items {
		r := kept.Items[i]
		other := r.SellerID
		if as == domain.Seller {
			other = r.BuyerID
		}
		items = append(items, OrderItem{
			OrderView: orderView(r.Order, as, places[r.Order.Recipient.CommuneCode], s.d.Waits), DealID: r.DealID,
			Listing: listingRef(listings[r.ListingID]), CounterpartID: other,
		})
	}
	return page.Page[OrderItem]{Items: items, NextCursor: kept.NextCursor}, nil
}

func (s *Deals) Order(ctx context.Context, id uuid.UUID) (OrderFacts, bool, error) {
	r, err := s.d.Deals.Order(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return OrderFacts{}, false, nil
	}
	if err != nil {
		return OrderFacts{}, false, fmt.Errorf("get order: %w", err)
	}
	o := r.Order
	return OrderFacts{
		BuyerID: r.BuyerID, SellerID: r.SellerID, ListingID: r.ListingID, Status: o.Status, Code: o.Code, Price: o.Price,
		Shipment: o.Shipment, CompletedBy: o.CompletedBy, CancelledBy: o.CancelledBy, CancelReason: o.CancelReason,
		RequestedAt: o.RequestedAt, ShippedAt: o.ShippedAt, ClosedAt: o.ClosedAt,
	}, true, nil
}
