package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/provider/carrier"
)

// ShipmentInput is a shipment as the seller sends it: Mode manual with the
// carrier and the code, or linked with the carrier, the service, the
// weight, the pickup and where from: a carrier's pickup point, one of the
// seller's addresses, or neither for the carrier's default.
type ShipmentInput struct {
	Mode            string
	Carrier         string
	TrackingCode    string
	Service         string
	WeightGrams     int
	Pickup          string
	PickupPointID   string
	PickupAddressID uuid.UUID
}

// CreateShipment checks the shipment and reads a manual one with the
// caller's account at its carrier, then finds the caller's order, runs
// DealShip once per caller and idempotencyKey, and reads the order's deal.
// A code the carrier does not know is domain.ErrShipmentUnknown; an
// unlinked carrier is domain.ErrCarrierNotLinked and a stack without a
// carrier domain.ErrCarrierUnavailable, for any order. A linked shipment's
// pickup address is copied now, so a later edit of it changes nothing.
func (s *Deals) CreateShipment(ctx context.Context, caller, order uuid.UUID, idempotencyKey string, in ShipmentInput) (DealView, error) {
	flow := FlowInput{Caller: caller, OrderID: order}
	if in.Mode == string(domain.ModeLinked) {
		linked, draftErr := domain.NewLinkedDraft(in.Carrier, in.Service, in.WeightGrams, in.Pickup)
		if draftErr != nil {
			return DealView{}, draftErr
		}
		from, fromErr := s.pickupFrom(ctx, caller, in.PickupPointID, in.PickupAddressID)
		if fromErr != nil {
			return DealView{}, fromErr
		}
		linked.From = from
		flow.Linked = &linked
	} else {
		c, code, trackErr := domain.Tracking(in.Carrier, in.TrackingCode)
		if trackErr != nil {
			return DealView{}, trackErr
		}
		p, readErr := s.shipping.Lookup(ctx, caller, carrier.Carrier(c), code)
		if readErr != nil {
			return DealView{}, shippingError(readErr, domain.ErrShipmentUnknown)
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
