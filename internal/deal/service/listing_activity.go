package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
)

// ListingActivity is what deal holds about one of a seller's listings: the
// buyers with a deal on it and the order the listing's card shows.
type ListingActivity struct {
	// Buyers holds each buyer with a deal on the listing and when that deal
	// opened, oldest first.
	Buyers []ListingBuyer
	// Order is nil while no order on the listing is pending or shipped.
	Order *ListingOrderView
}

// ListingBuyer is a buyer with a deal on a listing, since the deal opened.
type ListingBuyer struct {
	ID    uuid.UUID
	Since time.Time
}

// ListingOrderView is the ListingOrder schema in
// api/schemas/screen_my_listings.yaml.
type ListingOrderView struct {
	DealID  uuid.UUID          `json:"deal_id"`
	OrderID uuid.UUID          `json:"order_id"`
	Status  domain.OrderStatus `json:"status"`
}

// ListingActivity reads the deals on the seller's listings among ids in one
// query. Several buyers may each have a pending order on one listing; the
// card shows the shipped order, which holds the listing, else the pending
// order requested first, whose seller timeout ends first. A listing without
// a deal has no entry.
func (s *Deals) ListingActivity(ctx context.Context, seller uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]ListingActivity, error) {
	rows, err := s.deals.OnListings(ctx, seller, ids)
	if err != nil {
		return nil, fmt.Errorf("read deals on listings: %w", err)
	}
	out := map[uuid.UUID]ListingActivity{}
	shown := map[uuid.UUID]time.Time{} // when the order a card shows was requested
	for i := range rows {
		r := rows[i]
		a := out[r.ListingID]
		a.Buyers = append(a.Buyers, ListingBuyer{ID: r.BuyerID, Since: r.OpenedAt})
		if o := r.OpenOrder; o != nil {
			replace := a.Order == nil ||
				(a.Order.Status != domain.OrderShipped && (o.Status == domain.OrderShipped || o.RequestedAt.Before(shown[r.ListingID])))
			if replace {
				a.Order = &ListingOrderView{DealID: r.DealID, OrderID: o.ID, Status: o.Status}
				shown[r.ListingID] = o.RequestedAt
			}
		}
		out[r.ListingID] = a
	}
	return out, nil
}
