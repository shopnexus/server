package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/provider/carrier"
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
// from the carrier. A code the carrier does not know is
// domain.ErrShipmentNotFound; a stack without a carrier is
// domain.ErrCarrierUnavailable.
func (s *Deals) LookupShipment(ctx context.Context, carrierCode, trackingCode string) (ShipmentPreview, error) {
	c, code, err := domain.Tracking(carrierCode, trackingCode)
	if err != nil {
		return ShipmentPreview{}, err
	}
	p, err := s.carrier.Lookup(ctx, carrier.Carrier(c), code)
	switch {
	case errors.Is(err, carrier.ErrNotFound):
		return ShipmentPreview{}, domain.ErrShipmentNotFound
	case errors.Is(err, carrier.ErrUnavailable):
		return ShipmentPreview{}, domain.ErrCarrierUnavailable
	case err != nil:
		return ShipmentPreview{}, fmt.Errorf("read the shipment from %s: %w", c, err)
	}
	return ShipmentPreview{
		Carrier: c, TrackingCode: code, RecipientName: p.RecipientName, District: p.District, CODAmount: p.COD,
		Status: domain.ShipmentStatus(p.Status), Fee: p.Fee,
	}, nil
}
