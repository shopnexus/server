package service

import (
	"context"
	"errors"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/provider/carrier"
)

// ShipmentInput is a shipment as the seller sends it: manual with its carrier
// and code, or linked with its carrier, service, weight and pickup selection.
// The selection may name a carrier point, an owned address or saved pickup;
// omitting it uses the carrier's default.
type ShipmentInput struct {
	Mode            string
	Carrier         string
	TrackingCode    string
	Service         string
	WeightGrams     int
	Pickup          string
	PickupPointID   string
	PickupAddressID uuid.UUID
	SavedPickup     bool
}

// CreateShipment checks the shipment and reads a manual one with the
// caller's account at its carrier, then finds the caller's order, runs
// DealShip once per caller and idempotencyKey, and reads the order's deal.
// A code the carrier does not know is domain.ErrShipmentUnknown and a stack
// without a carrier domain.ErrCarrierUnavailable, for any order; a carrier
// the caller has not linked is domain.ErrCarrierNotLinked once the order
// is the caller's to ship. CreateShipment copies an explicit book address
// before the flow. DealShip resolves saved pickup in a journaled step after
// its order checks.
func (s *Deals) CreateShipment(ctx context.Context, caller, order uuid.UUID, idempotencyKey string, in ShipmentInput) (DealView, error) {
	flow := FlowInput{Caller: caller, OrderID: order}
	if in.Mode == string(domain.ModeLinked) {
		linked, draftErr := domain.NewLinkedDraft(in.Carrier, in.Service, in.WeightGrams, in.Pickup)
		if draftErr != nil {
			return DealView{}, draftErr
		}
		if in.SavedPickup && (in.PickupPointID != "" || in.PickupAddressID != uuid.Nil()) {
			return DealView{}, domain.ErrInvalidShipment.WithField("pickup_from", "oneOf")
		}
		from, fromErr := s.pickupFrom(ctx, caller, in.PickupPointID, in.PickupAddressID)
		if fromErr != nil {
			return DealView{}, fromErr
		}
		flow.SavedPickup = in.SavedPickup
		linked.From = from
		flow.Linked = &linked
	} else {
		c, code, trackErr := domain.Tracking(in.Carrier, in.TrackingCode)
		if trackErr != nil {
			return DealView{}, trackErr
		}
		p, readErr := s.shipping.Lookup(ctx, caller, carrier.Carrier(c), code)
		readErr = shippingError(readErr, domain.ErrShipmentUnknown)
		if readErr != nil && !errors.Is(readErr, domain.ErrCarrierNotLinked) {
			return DealView{}, readErr
		}
		// A stranger, or the buyer, links nothing: their order answer
		// comes before the carrier's.
		if readErr != nil {
			if err := s.checkSeller(ctx, caller, order); err != nil {
				return DealView{}, err
			}
			return DealView{}, readErr
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
