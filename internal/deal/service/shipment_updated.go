package service

import (
	"context"
	"fmt"
	"time"

	"github.com/shopnexus/server/internal/deal/domain"
)

// ShipmentUpdated is UC-DEAL-05, a carrier's report: it checks status and
// runs ShipmentUpdates for the parcel at carrier c, of a step that happened
// at (zero for now), with the carrier's newest fee, nil for none. The same
// report twice changes nothing the second time (A1), and neither does one
// older than the order's.
func (s *Deals) ShipmentUpdated(ctx context.Context, c, trackingCode, status string, at time.Time, fee *int64) error {
	st, err := domain.ParseShipmentStatus(status)
	if err != nil {
		return err
	}
	in := ShipmentSignal{Carrier: domain.Carrier(c), TrackingCode: trackingCode, Status: st, At: at, Fee: fee}
	if _, err = s.shipment.Call(ctx, in); err != nil {
		return err
	}
	return nil
}

func (s *Deals) ShipmentKnown(ctx context.Context, c, trackingCode string) (bool, error) {
	has, err := s.deals.HasShipment(ctx, c, trackingCode)
	if err != nil {
		return false, fmt.Errorf("find the shipment: %w", err)
	}
	return has, nil
}
