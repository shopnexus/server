package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
)

// Ship checks the waybill, runs DealShip once per caller and
// idempotencyKey, then reads the deal.
func (s *Deals) Ship(ctx context.Context, caller, id uuid.UUID, idempotencyKey string, w WaybillInput) (DealView, error) {
	waybill, err := domain.NewWaybill(domain.Carrier(w.Carrier), w.CarrierName, w.TrackingCode, w.Fee)
	if err != nil {
		return DealView{}, err
	}
	if _, err = s.ship.Call(ctx, caller.String()+"/"+idempotencyKey, FlowInput{Caller: caller, DealID: id, Waybill: &waybill}); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, id)
}

// Deliver runs DealFinish with delivered once per caller and
// idempotencyKey, then reads the deal.
func (s *Deals) Deliver(ctx context.Context, caller, id uuid.UUID, idempotencyKey string) (DealView, error) {
	in := FlowInput{Caller: caller, DealID: id, Outcome: domain.OrderDelivered}
	if _, err := s.finish.Call(ctx, caller.String()+"/"+idempotencyKey, in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, id)
}

// Return runs DealFinish with returned once per caller and idempotencyKey,
// then reads the deal.
func (s *Deals) Return(ctx context.Context, caller, id uuid.UUID, idempotencyKey string) (DealView, error) {
	in := FlowInput{Caller: caller, DealID: id, Outcome: domain.OrderReturned}
	if _, err := s.finish.Call(ctx, caller.String()+"/"+idempotencyKey, in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, id)
}
