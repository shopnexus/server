package service

import (
	"context"
	"uuid"
)

// CancelOrder is UC-DEAL-06: it finds the caller's order, runs
// DealCancelOrder, then reads the order's deal.
func (s *Deals) CancelOrder(ctx context.Context, caller, order, key uuid.UUID) (DealView, error) {
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	in := FlowInput{Caller: caller, Key: key, DealID: deal, OrderID: order}
	if _, err = s.cancelOrder.Call(ctx, "", in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, deal)
}
