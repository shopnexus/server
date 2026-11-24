package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/provider/carrier"
)

// ShipmentPreview is the ShipmentPreview schema in api/schemas/deal.yaml.
type ShipmentPreview struct {
	Carrier       domain.Carrier        `json:"carrier"`
	TrackingCode  string                `json:"tracking_code"`
	RecipientName string                `json:"recipient_name"`
	District      string                `json:"district"`
	CODAmount     int64                 `json:"cod_amount"`
	Status        domain.ShipmentStatus `json:"status"`
	Fee           *int64                `json:"fee,omitempty"`
}

// LookupShipment checks the carrier and the code and reads the shipment
// with the caller's account at the carrier. A code the account does not
// know is domain.ErrShipmentNotFound; a carrier the caller has not linked
// is domain.ErrCarrierNotLinked; a stack without a carrier is
// domain.ErrCarrierUnavailable.
func (s *Deals) LookupShipment(ctx context.Context, caller uuid.UUID, carrierCode, trackingCode string) (ShipmentPreview, error) {
	c, code, err := domain.Tracking(carrierCode, trackingCode)
	if err != nil {
		return ShipmentPreview{}, err
	}
	p, err := s.shipping.Lookup(ctx, caller, carrier.Carrier(c), code)
	if err != nil {
		return ShipmentPreview{}, shippingError(err, domain.ErrShipmentNotFound)
	}
	return ShipmentPreview{
		Carrier: c, TrackingCode: code, RecipientName: p.RecipientName, District: p.District, CODAmount: p.COD,
		Status: domain.ShipmentStatus(p.Status()), Fee: p.Fee,
	}, nil
}
