package service

import (
	"context"
	"fmt"
	"time"
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

func (s *Deals) CountOrders(ctx context.Context, since time.Time) (map[domain.OrderStatus]int, error) {
	n, err := s.deals.CountOrders(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("count orders: %w", err)
	}
	return n, nil
}
