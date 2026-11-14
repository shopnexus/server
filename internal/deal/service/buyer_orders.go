package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
)

// BuyerOrders counts a buyer's orders, for the figures and the COD cap of
// the reputation view.
type BuyerOrders struct {
	// Placed and Failed count the orders requested over the last
	// domain.BuyerPeriod and the failed ones among them.
	Placed, Failed int
	// Open counts the orders pending or shipped, against
	// domain.MaxOpenOrders.
	Open int
}

func (s *Deals) BuyerOrders(ctx context.Context, buyer uuid.UUID) (BuyerOrders, error) {
	n, err := s.deals.BuyerOrders(ctx, buyer, s.now().Add(-domain.BuyerPeriod))
	if err != nil {
		return BuyerOrders{}, fmt.Errorf("count orders: %w", err)
	}
	return BuyerOrders{Placed: n.Placed, Failed: n.Failed, Open: n.Open}, nil
}
