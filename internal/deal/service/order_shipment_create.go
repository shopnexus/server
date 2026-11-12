package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/provider/carrier"
)

// ShipmentInput is a shipment as the seller sends it: Mode manual with the
// carrier and the code, or linked with the service, the weight and the
// pickup.
type ShipmentInput struct {
	Mode         string
	Carrier      string
	TrackingCode string
	Service      string
	WeightGrams  int
	Pickup       string
}

// CreateShipment checks the shipment and reads a manual one from its
// carrier, then finds the caller's order, runs DealShip once per caller and
// idempotencyKey, and reads the order's deal. A code the carrier does not
// know is domain.ErrShipmentUnknown; a stack without a carrier is
// domain.ErrCarrierUnavailable, for any order.
func (s *Deals) CreateShipment(ctx context.Context, caller, order uuid.UUID, idempotencyKey string, in ShipmentInput) (DealView, error) {
	flow := FlowInput{Caller: caller, OrderID: order}
	if in.Mode == string(domain.ModeLinked) {
		linked, draftErr := domain.NewLinkedDraft(in.Service, in.WeightGrams, in.Pickup)
		if draftErr != nil {
			return DealView{}, draftErr
		}
		flow.Linked = &linked
	} else {
		c, code, trackErr := domain.Tracking(in.Carrier, in.TrackingCode)
		if trackErr != nil {
			return DealView{}, trackErr
		}
		p, readErr := s.carrier.Lookup(ctx, carrier.Carrier(c), code)
		switch {
		case errors.Is(readErr, carrier.ErrNotFound):
			return DealView{}, domain.ErrShipmentUnknown
		case errors.Is(readErr, carrier.ErrUnavailable):
			return DealView{}, domain.ErrCarrierUnavailable
		case readErr != nil:
			return DealView{}, fmt.Errorf("read the shipment from %s: %w", c, readErr)
		}
		flow.Manual = &domain.Shipment{
			Mode: domain.ModeManual, Carrier: c, TrackingCode: code, Fee: p.Fee, Status: domain.ShipmentStatus(p.Status),
		}
	}
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	flow.DealID = deal
	if _, err = s.ship.Call(ctx, caller.String()+"/"+idempotencyKey, flow); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, deal)
}
