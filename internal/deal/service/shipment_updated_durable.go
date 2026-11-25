package service

import (
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// shipmentUpdates is the ShipmentUpdates service's name without the prefix.
const shipmentUpdates = "ShipmentUpdates"

// ShipmentSignal is a carrier's word on a shipment: the carrier, the
// tracking code there, its new status and when the carrier's step
// happened, zero for the time the signal is handled. Fee is the carrier's
// newest fee, nil when it told none.
type ShipmentSignal struct {
	Carrier      domain.Carrier        `json:"carrier"`
	TrackingCode string                `json:"tracking_code"`
	Status       domain.ShipmentStatus `json:"status"`
	At           time.Time             `json:"at,omitzero"`
	Fee          *int64                `json:"fee,omitempty"`
}

// ShipmentResult carries ShipmentUpdates' refusal, or none.
type ShipmentResult struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r ShipmentResult) Refusal() *apperr.Error { return r.Refused }

// ShipmentUpdates is the one entry point of a carrier's word on a
// shipment: the carriers' webhooks, the poll and the dev endpoint. It
// moves every shipped order whose shipment is that parcel, two items in
// one parcel included, and changes nothing on a closed order or with a
// step no newer than the order's; a signal's fee replaces the order's
// without a move. A failure releases the listing, a delivery schedules
// the completion and a pickup the transit timeout.
type ShipmentUpdates struct{ Deals *Deals }

func (s ShipmentUpdates) ServiceName() string { return s.Deals.prefix + shipmentUpdates }

// Run is UC-DEAL-05, steps 1 and 4, for every order on the parcel.
func (s ShipmentUpdates) Run(ctx restate.Context, in ShipmentSignal) (ShipmentResult, error) {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return ShipmentResult{}, err
	}
	stepAt := in.At
	if stepAt.IsZero() {
		stepAt = at
	}
	// The store keeps microseconds, so a resend compares equal.
	stepAt = stepAt.Truncate(time.Microsecond)
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
			var before domain.Shipment
			if o := d.Order(); o != nil && o.Shipment != nil {
				before = *o.Shipment
			}
			// A1 · NFR-IDEM-01: the same report twice, or an older one,
			// changes nothing. The fee goes first, so a failing report
			// still lands it.
			feeChanged := in.Fee != nil && d.FeeReported(ref.OrderID, *in.Fee, stepAt)
			changed, moveErr := d.ShipmentReported(ref.OrderID, in.Status, stepAt, at)
			if moveErr != nil || (!changed && !feeChanged) {
				break // a closed order, or nothing new
			}
			refused, saveErr := saveStep(ctx, s.Deals.deals, &d, uuid.Nil(), at)
			if saveErr != nil {
				return ShipmentResult{}, saveErr
			}
			if refused != nil {
				continue // the deal moved meanwhile: read it again
			}
			// E2 · BR-DEAL-08: a parcel refused at the door, or lost after
			// the pickup, fails the order and puts the listing back on sale.
			after := d.Order().Shipment
			switch {
			case d.Order().Status == domain.OrderFailed:
				if err = releaseStep(ctx, s.Deals.catalog, m.Listing.ID, ref.OrderID); err != nil {
					return ShipmentResult{}, err
				}
			case after.Status == before.Status: // a newer step of the same status
			case after.Status == domain.ShipmentDelivered: // A4
				restate.ServiceSend(ctx, s.Deals.prefix+orderTimers, "AutoComplete").
					Send(ref, restate.WithDelay(s.Deals.waits.Complete))
			case after.Status == domain.ShipmentInTransit:
				restate.ServiceSend(ctx, s.Deals.prefix+orderTimers, "TransitTimeout").
					Send(ref, restate.WithDelay(s.Deals.waits.Transit))
			default: // pending pickup, or lost before it: PickupTimeout waits
			}
			break
		}
	}
	return ShipmentResult{}, nil
}
