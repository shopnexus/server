package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// ShipmentInput is a shipment as the seller sends it: its carrier,
// service, weight and pickup selection. The selection may name a carrier
// point, an owned address or saved pickup; omitting it uses the carrier's
// default.
type ShipmentInput struct {
	Carrier         string
	Service         string
	WeightGrams     int
	Pickup          string
	PickupPointID   string
	PickupAddressID uuid.UUID
	SavedPickup     bool
}

// CreateShipment is UC-DEAL-04, step 1: it checks the draft, then runs
// DealShip on the caller's order once per caller and idempotencyKey (A1,
// NFR-IDEM-01). It copies an explicit book address before the flow;
// DealShip resolves a saved pickup in a journaled step after its order
// checks.
func (s *Deals) CreateShipment(ctx context.Context, caller, order uuid.UUID, idempotencyKey string, in ShipmentInput) (DealView, error) {
	// E6: a draft that breaks a rule.
	draft, err := domain.NewShipmentDraft(in.Carrier, in.Service, in.WeightGrams, in.Pickup)
	if err != nil {
		return DealView{}, err
	}
	if in.SavedPickup && (in.PickupPointID != "" || in.PickupAddressID != uuid.Nil()) {
		return DealView{}, domain.ErrInvalidShipment.WithField("pickup_from", apperr.OneOf, "oneOf")
	}
	if draft.From, err = s.pickupFrom(ctx, caller, in.PickupPointID, in.PickupAddressID); err != nil {
		return DealView{}, err
	}
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	flow := FlowInput{Caller: caller, DealID: deal, OrderID: order, Draft: &draft, SavedPickup: in.SavedPickup}
	if _, err = s.ship.Call(ctx, invocationKey(caller, idempotencyKey), flow); err != nil {
		return DealView{}, err
	}
	return s.Get(ctx, caller, deal)
}
