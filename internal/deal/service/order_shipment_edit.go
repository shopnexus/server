package service

import (
	"context"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/deal/domain"
)

// EditShipment is UC-DEAL-04 A3: it checks the corrected carrier and code
// and reads them with the caller's account at the carrier, then finds the
// caller's order and puts them on its manual shipment. A code the carrier
// does not know is domain.ErrShipmentUnknown and a stack without a carrier
// domain.ErrCarrierUnavailable, for any order; a carrier the caller has not
// linked is domain.ErrCarrierNotLinked once the order is the caller's to
// ship; a deal the other side moved meanwhile is domain.ErrChanged.
func (s *Deals) EditShipment(ctx context.Context, caller, order uuid.UUID, carrierCode, trackingCode string) (DealView, error) {
	manual, err := s.manualShipment(ctx, caller, order, carrierCode, trackingCode)
	if err != nil {
		return DealView{}, err
	}
	deal, err := s.orderDeal(ctx, caller, order)
	if err != nil {
		return DealView{}, err
	}
	m, err := s.load(ctx, caller, deal, false)
	if err != nil {
		return DealView{}, err
	}
	if err = m.deal.EditShipment(m.by, order, manual, m.now); err != nil {
		return DealView{}, err
	}
	if m.deal.Order().Shipment.Status == domain.ShipmentDelivered {
		// The completion goes before the save; one that finds the parcel
		// not delivered, as after a refused save, does nothing.
		if _, err = ingress.ServiceSend[OrderRef](s.ingress, s.prefix+orderTimers, "AutoComplete").
			Send(ctx, OrderRef{DealID: deal, OrderID: order}, restate.WithDelay(s.waits.Complete)); err != nil {
			return DealView{}, fmt.Errorf("schedule the completion: %w", err)
		}
	}
	return s.save(ctx, caller, m)
}
