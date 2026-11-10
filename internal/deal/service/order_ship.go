package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
)

// Ship checks the waybill, runs DealShip on the caller's order once per
// caller and idempotencyKey, then reads the order's deal.
func (s *Deals) Ship(ctx context.Context, caller, order uuid.UUID, idempotencyKey string, w WaybillInput) (DealView, error) {
	waybill, err := domain.NewWaybill(domain.Carrier(w.Carrier), w.CarrierName, w.TrackingCode, w.Fee)
	if err != nil {
		return DealView{}, err
	}
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	in := FlowInput{Caller: caller, DealID: deal, OrderID: order, Waybill: &waybill}
	if _, err = s.ship.Call(ctx, caller.String()+"/"+idempotencyKey, in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, deal)
}

// Deliver runs DealFinish with delivered on the caller's order once per
// caller and idempotencyKey, then reads the order's deal.
func (s *Deals) Deliver(ctx context.Context, caller, order uuid.UUID, idempotencyKey string) (DealView, error) {
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	in := FlowInput{Caller: caller, DealID: deal, OrderID: order, Outcome: domain.OrderDelivered}
	if _, err = s.finish.Call(ctx, caller.String()+"/"+idempotencyKey, in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, deal)
}

// Return runs DealFinish with returned on the caller's order once per
// caller and idempotencyKey, then reads the order's deal.
func (s *Deals) Return(ctx context.Context, caller, order uuid.UUID, idempotencyKey string) (DealView, error) {
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	in := FlowInput{Caller: caller, DealID: deal, OrderID: order, Outcome: domain.OrderReturned}
	if _, err = s.finish.Call(ctx, caller.String()+"/"+idempotencyKey, in); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, deal)
}

// orderDeal is the deal of order. An unknown order and one the caller is
// neither side of are both domain.ErrOrderNotFound, so an order's existence
// does not leak.
func (s *Deals) orderDeal(ctx context.Context, caller, order uuid.UUID) (uuid.UUID, error) {
	r, err := s.deals.Order(ctx, order)
	if errors.Is(err, repository.ErrNotFound) {
		return uuid.Nil(), domain.ErrOrderNotFound
	}
	if err != nil {
		return uuid.Nil(), fmt.Errorf("get order: %w", err)
	}
	if caller != r.BuyerID && caller != r.SellerID {
		return uuid.Nil(), domain.ErrOrderNotFound
	}
	return r.DealID, nil
}
