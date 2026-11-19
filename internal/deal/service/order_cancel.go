package service

import (
	"context"
	"uuid"
)

// CancelOrder is UC-DEAL-06: it finds the caller's order, runs
// DealCancelOrder once per caller and idempotencyKey (NFR-IDEM-01), or on
// every call without a key, then reads the order's deal.
func (s *Deals) CancelOrder(ctx context.Context, caller, order uuid.UUID, idempotencyKey string) (DealView, error) {
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	in := FlowInput{Caller: caller, DealID: deal, OrderID: order}
	if _, err = s.cancelOrder.Call(ctx, invocationKey(caller, idempotencyKey), in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, deal)
}
