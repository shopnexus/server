package service

import (
	"context"
	"uuid"
)

// CancelOrder is UC-DEAL-06.
func (s *Deals) CancelOrder(ctx context.Context, caller, order uuid.UUID) (DealView, error) {
	id, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	m, err := s.load(ctx, caller, id, false)
	if err != nil {
		return DealView{}, err
	}
	// E1: only a pending order cancels; a shipped one is
	// domain.ErrMoveNotAllowed.
	if err = m.deal.CancelPendingOrder(m.by, order, m.now); err != nil {
		return DealView{}, err
	}
	return s.save(ctx, caller, m)
}
