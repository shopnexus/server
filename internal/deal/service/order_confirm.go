package service

import (
	"context"
	"uuid"
)

// ConfirmReceipt is UC-DEAL-05 A2: it finds the caller's order, runs
// DealConfirm on it once per caller and idempotencyKey (NFR-IDEM-01), then
// reads the order's deal.
func (s *Deals) ConfirmReceipt(ctx context.Context, caller, order uuid.UUID, idempotencyKey string) (DealView, error) {
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	in := FlowInput{Caller: caller, DealID: deal, OrderID: order}
	if _, err = s.confirm.Call(ctx, caller.String()+"/"+idempotencyKey, in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, deal)
}
