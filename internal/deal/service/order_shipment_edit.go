package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/provider/carrier"
)

// EditShipment checks the corrected carrier and code and reads them from
// the carrier, then finds the caller's order and puts them on its manual
// shipment. A code the carrier does not know is domain.ErrShipmentUnknown;
// a stack without a carrier is domain.ErrCarrierUnavailable, for any
// order; a deal the other side moved meanwhile is domain.ErrChanged.
func (s *Deals) EditShipment(ctx context.Context, caller, order uuid.UUID, carrierCode, trackingCode string) (DealView, error) {
	c, code, err := domain.Tracking(carrierCode, trackingCode)
	if err != nil {
		return DealView{}, err
	}
	p, err := s.carrier.Lookup(ctx, carrier.Carrier(c), code)
	switch {
	case errors.Is(err, carrier.ErrNotFound):
		return DealView{}, domain.ErrShipmentUnknown
	case errors.Is(err, carrier.ErrUnavailable):
		return DealView{}, domain.ErrCarrierUnavailable
	case err != nil:
		return DealView{}, fmt.Errorf("read the shipment from %s: %w", c, err)
	}
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	m, err := s.load(ctx, caller, deal, false)
	if err != nil {
		return DealView{}, err
	}
	if err = m.deal.EditShipment(m.by, order, domain.Shipment{
		Mode: domain.ModeManual, Carrier: c, TrackingCode: code, Fee: p.Fee, Status: domain.ShipmentStatus(p.Status),
	}, m.now); err != nil {
		return DealView{}, err
	}
	return s.save(ctx, caller, m)
}
