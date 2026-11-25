package service

import (
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// shipmentUpdates is the ShipmentUpdates service's name without the prefix.
const shipmentUpdates = "ShipmentUpdates"

// ShipmentSignal is a carrier's word on a shipment: the carrier, the
// tracking code there and its new status.
type ShipmentSignal struct {
	Carrier      domain.Carrier        `json:"carrier"`
	TrackingCode string                `json:"tracking_code"`
	Status       domain.ShipmentStatus `json:"status"`
}

// ShipmentResult carries ShipmentUpdates' refusal, or none.
type ShipmentResult struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r ShipmentResult) Refusal() *apperr.Error { return r.Refused }

// ShipmentUpdates is the one entry point of a carrier's word on a
// shipment: the carriers' webhooks, the poll and the dev endpoint. It
// moves every shipped order whose shipment is that parcel, two items in
// one parcel included, and changes nothing on a closed order.
// A failure releases the listing; a delivery schedules the completion.
type ShipmentUpdates struct{ Deals *Deals }

func (s ShipmentUpdates) ServiceName() string { return s.Deals.prefix + shipmentUpdates }

// Run is UC-DEAL-05, steps 1 and 4, for every order on the parcel.
func (s ShipmentUpdates) Run(ctx restate.Context, in ShipmentSignal) (ShipmentResult, error) {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return ShipmentResult{}, err
	}
	refs, err := restate.Run(ctx, func(rc restate.RunContext) ([]OrderRef, error) {
		rows, stepErr := s.Deals.deals.OrdersWithCode(rc, string(in.Carrier), in.TrackingCode)
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
			// A1 · NFR-IDEM-01: the same status twice changes nothing.
			changed, moveErr := d.ShipmentUpdated(ref.OrderID, in.Status, at)
			if moveErr != nil || !changed {
				break // a closed order, or nothing new
			}
			refused, saveErr := saveStep(ctx, s.Deals.deals, &d, uuid.Nil(), at)
			if saveErr != nil {
				return ShipmentResult{}, saveErr
			}
			if refused != nil {
				continue // the deal moved meanwhile: read it again
			}
			// E2 · BR-DEAL-08: a parcel refused at the door fails the order
			// and puts the listing back on sale.
			if d.Order().Status == domain.OrderFailed {
				if err = releaseStep(ctx, s.Deals.catalog, m.Listing.ID, ref.OrderID); err != nil {
					return ShipmentResult{}, err
				}
			} else if in.Status == domain.ShipmentDelivered { // A4
				restate.ServiceSend(ctx, s.Deals.prefix+orderTimers, "AutoComplete").
					Send(ref, restate.WithDelay(s.Deals.waits.Complete))
			}
			break
		}
	}
	return ShipmentResult{}, nil
}
