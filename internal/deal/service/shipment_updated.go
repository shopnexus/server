package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
)

// ShipmentUpdated is UC-DEAL-05, a carrier's report: it checks status and
// runs ShipmentUpdates for trackingCode. A report is not idempotent, so
// each call takes a key of its own; the same status twice changes nothing
// the second time (A1).
func (s *Deals) ShipmentUpdated(ctx context.Context, trackingCode, status string) error {
	st, err := domain.ParseShipmentStatus(status)
	if err != nil {
		return err
	}
	in := ShipmentSignal{TrackingCode: trackingCode, Status: st}
	if _, err = s.shipment.Call(ctx, uuid.NewV7().String(), in); err != nil {
		return err
	}
	return nil
}
