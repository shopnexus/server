package service

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// shipmentUpdates is the ShipmentUpdates service's name without the prefix.
const shipmentUpdates = "ShipmentUpdates"

// ShipmentSignal is a carrier's word on a shipment: its tracking code and
// its new status.
type ShipmentSignal struct {
	TrackingCode string                `json:"tracking_code"`
	Status       domain.ShipmentStatus `json:"status"`
}

// ShipmentResult carries ShipmentUpdates' refusal, or none.
type ShipmentResult struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r ShipmentResult) Refusal() *apperr.Error { return r.Refused }

// ShipmentUpdates is the one entry point of a carrier's word on a
// shipment: the dev endpoint calls it, and the carriers' webhooks and
// polling will. It moves every shipped order whose shipment has the code,
// two items in one parcel included, and changes nothing on a closed order.
// A failure releases the listing; a delivery schedules the completion.
type ShipmentUpdates struct{ Deals *Deals }

func (s ShipmentUpdates) ServiceName() string { return s.Deals.prefix + shipmentUpdates }

func (s ShipmentUpdates) Run(ctx restate.Context, in ShipmentSignal) (ShipmentResult, error) {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return ShipmentResult{}, err
	}
	refs, err := restate.Run(ctx, func(rc restate.RunContext) ([]OrderRef, error) {
		rows, stepErr := s.Deals.deals.OrdersWithCode(rc, in.TrackingCode)
		if stepErr != nil {
			return nil, fmt.Errorf("find the orders: %w", stepErr)
		}
		out := make([]OrderRef, 0, len(rows))
		for i := range rows {
			out = append(out, OrderRef{DealID: rows[i].DealID, OrderID: rows[i].Order.ID})
		}
		return out, nil
	})
	if err != nil {
		return ShipmentResult{}, fmt.Errorf("find the shipment's orders: %w", err)
	}
	if len(refs) == 0 {
		return ShipmentResult{Refused: domain.ErrShipmentNotFound}, nil
	}
	for _, ref := range refs {
		for {
			m, loadErr := systemLoadStep(ctx, s.Deals, ref.DealID)
			if loadErr != nil {
				return ShipmentResult{}, loadErr
			}
			d := domain.RestoreDeal(m.Deal)
			changed, moveErr := d.ShipmentUpdated(ref.OrderID, in.Status, at)
			if moveErr != nil || !changed {
				break // a closed order, or nothing new
			}
			refused, saveErr := saveStep(ctx, s.Deals.deals, &d, restatex.NewID(ctx, at), restatex.NewID(ctx, at), at)
			if saveErr != nil {
				return ShipmentResult{}, saveErr
			}
			if refused != nil {
				continue // the deal moved meanwhile: read it again
			}
			if d.Order().Status == domain.OrderFailed {
				if err = releaseStep(ctx, s.Deals.catalog, m.Listing.ID, ref.OrderID); err != nil {
					return ShipmentResult{}, err
				}
			} else if in.Status == domain.ShipmentDelivered {
				restate.ServiceSend(ctx, s.Deals.prefix+orderTimers, "AutoComplete").
					Send(ref, restate.WithDelay(s.Deals.waits.Complete))
			}
			break
		}
	}
	return ShipmentResult{}, nil
}
