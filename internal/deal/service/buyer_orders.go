package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
)

func (s *Deals) BuyerOrders(ctx context.Context, buyer uuid.UUID) (domain.BuyerOrders, error) {
	n, err := s.deals.BuyerOrders(ctx, buyer, s.now().Add(-domain.BuyerPeriod))
	if err != nil {
		return domain.BuyerOrders{}, fmt.Errorf("count orders: %w", err)
	}
	return n, nil
}
