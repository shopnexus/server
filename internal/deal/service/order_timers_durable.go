package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/carrier"
)

// orderTimers is the OrderTimers service's name without the prefix.
const orderTimers = "OrderTimers"

// OrderRef names the order a timer is for and its deal. Restate keeps it
// as the timer's input for the whole wait, so its JSON stays the same
// across releases.
type OrderRef struct {
	DealID  uuid.UUID `json:"deal_id"`
	OrderID uuid.UUID `json:"order_id"`
}

// OrderTimers moves an order on once a wait ends. Each handler is called
// by a send with a delay: it reads the deal when it fires, does nothing
// when the order moved on, and never compares clocks. A save that loses a
// race reads the deal again.
type OrderTimers struct{ Deals *Deals }

func (s OrderTimers) ServiceName() string { return s.Deals.d.Prefix + orderTimers }

// SellerTimeout is UC-DEAL-06 A1: it cancels a pending order whose seller
// made no shipment within Waits.Seller. A pending order holds no listing.
func (s OrderTimers) SellerTimeout(ctx restate.Context, ref OrderRef) error {
	at, err := restatex.Now(ctx, s.Deals.d.Now)
	if err != nil {
		return err
	}
	_, _, _, err = s.timeout(ctx, ref, at, (*domain.Deal).SellerTimeout)
	return err
}

// PickupTimeout is UC-DEAL-06 A2: it cancels a shipped order whose parcel
// the carrier had not picked up Waits.Pickup after the shipment, gives the
// listing back, then calls a linked shipment off at its carrier. It reads
// the carrier first: a parcel picked up since the last poll moves the order
// instead. A carrier that has the parcel by the call off, or no longer
// knows it, leaves the order cancelled all the same.
func (s OrderTimers) PickupTimeout(ctx restate.Context, ref OrderRef) error {
	at, err := restatex.Now(ctx, s.Deals.d.Now)
	if err != nil {
		return err
	}
	m, err := systemLoadStep(ctx, s.Deals, ref.DealID)
	if err != nil {
		return err
	}
	first := domain.RestoreDeal(m.Deal)
	if o := first.Order(); o != nil && o.ID == ref.OrderID && o.Status == domain.OrderShipped && o.Shipment != nil {
		moved, readErr := s.readMoved(ctx, m.Deal.SellerID, *o.Shipment)
		if readErr != nil || moved {
			return readErr
		}
	}
	m, d, moved, err := s.timeout(ctx, ref, at, (*domain.Deal).PickupTimeout)
	if err != nil || !moved {
		return err
	}
	if err = releaseStep(ctx, s.Deals.d.Catalog, m.Listing.ID, ref.OrderID); err != nil {
		return err
	}
	if sh := d.Order().Shipment; sh.Mode == domain.ModeLinked {
		return callOffStep(ctx, s.Deals.d.Shipping, s.Deals.d.Log, m.Deal.SellerID, *sh)
	}
	return nil
}

// AutoComplete is UC-DEAL-05 A4: it completes a shipped order
// Waits.Complete after its carrier reported the parcel delivered, unless
// the buyer confirmed first, and marks the listing sold.
func (s OrderTimers) AutoComplete(ctx restate.Context, ref OrderRef) error {
	at, err := restatex.Now(ctx, s.Deals.d.Now)
	if err != nil {
		return err
	}
	m, _, moved, err := s.timeout(ctx, ref, at, (*domain.Deal).AutoComplete)
	if err != nil || !moved {
		return err
	}
	return sellStep(ctx, s.Deals.d.Catalog, m.Listing.ID, ref.OrderID, at)
}

// Poll is UC-DEAL-05, step 1, when no report comes: it reads a shipped
// order's shipment from its carrier with the seller's account and moves the
// order as a report would, then reads it again Waits.Poll later while the
// order stays shipped. It is the safety net under the carriers' reports: a
// report that came first leaves it nothing to change, and a code the
// carrier lost or a carrier unlinked waits for the next read.
func (s OrderTimers) Poll(ctx restate.Context, ref OrderRef) error {
	m, err := systemLoadStep(ctx, s.Deals, ref.DealID)
	if err != nil {
		return err
	}
	d := domain.RestoreDeal(m.Deal)
	o := d.Order()
	if o == nil || o.ID != ref.OrderID || o.Status != domain.OrderShipped || o.Shipment == nil {
		return nil // the order moved on
	}
	if _, err = s.readMoved(ctx, m.Deal.SellerID, *o.Shipment); err != nil {
		return err
	}
	restate.ServiceSend(ctx, s.ServiceName(), "Poll").Send(ref, restate.WithDelay(s.Deals.d.PollEvery))
	return nil
}

// TransitTimeout is UC-DEAL-05 E4: it fails a shipped order whose
// carrier, Waits.Transit after the parcel went in transit, no longer knows
// it or reports it cancelled or lost; it gives the listing back. A carrier
// still moving the parcel moves the order as a report would, and the
// timeout waits again. A read that tells nothing, such as a carrier to
// link again, waits again too.
func (s OrderTimers) TransitTimeout(ctx restate.Context, ref OrderRef) error {
	at, err := restatex.Now(ctx, s.Deals.d.Now)
	if err != nil {
		return err
	}
	m, err := systemLoadStep(ctx, s.Deals, ref.DealID)
	if err != nil {
		return err
	}
	first := domain.RestoreDeal(m.Deal)
	o := first.Order()
	if o == nil || o.ID != ref.OrderID || o.Status != domain.OrderShipped || o.Shipment == nil ||
		o.Shipment.Status == domain.ShipmentDelivered {
		return nil // the order moved on
	}
	r, err := s.read(ctx, m.Deal.SellerID, *o.Shipment)
	if err != nil {
		return err
	}
	if !r.Gone {
		if (r.Status != "" && r.Status != o.Shipment.Status) || r.newFee(*o.Shipment) {
			if err = s.move(ctx, *o.Shipment, r); err != nil {
				return err
			}
		}
		restate.ServiceSend(ctx, s.ServiceName(), "TransitTimeout").Send(ref, restate.WithDelay(s.Deals.d.Waits.Transit))
		return nil
	}
	m, _, moved, err := s.timeout(ctx, ref, at, (*domain.Deal).TransitTimeout)
	if err != nil || !moved {
		return err
	}
	return releaseStep(ctx, s.Deals.d.Catalog, m.Listing.ID, ref.OrderID)
}

// timeout reads the deal, moves ref's order with move and saves it, reading
// it again while a save loses a race. moved is false when the order moved
// on already.
func (s OrderTimers) timeout(ctx restate.Context, ref OrderRef, at time.Time,
	move func(d *domain.Deal, order uuid.UUID, now time.Time) error,
) (m loaded, d domain.Deal, moved bool, err error) {
	for {
		if m, err = systemLoadStep(ctx, s.Deals, ref.DealID); err != nil {
			return m, d, false, err
		}
		d = domain.RestoreDeal(m.Deal)
		if errors.Is(move(&d, ref.OrderID, at), domain.ErrMoveNotAllowed) {
			return m, d, false, nil
		}
		refused, saveErr := saveStep(ctx, s.Deals.d.Deals, &d, uuid.Nil(), at)
		if saveErr != nil || refused == nil {
			return m, d, saveErr == nil, saveErr
		}
	}
}

// carrierRead is what a read of a shipment at its carrier journals: the
// status its newest step moves an order to and when the step happened,
// empty when it tells none, the newest fee, nil for none, or Gone for a
// parcel the carrier no longer knows or reports cancelled or lost.
type carrierRead struct {
	Status domain.ShipmentStatus `json:"status,omitempty"`
	At     time.Time             `json:"at,omitzero"`
	Fee    *int64                `json:"fee,omitempty"`
	Gone   bool                  `json:"gone,omitempty"`
}

// newFee reports whether r tells a fee sh does not have.
func (r carrierRead) newFee(sh domain.Shipment) bool {
	return r.Status != "" && r.Fee != nil && (sh.Fee == nil || *sh.Fee != *r.Fee)
}

// read reads sh from its carrier with the seller's account in one
// journaled step. A refusal of shipping's, such as a carrier to link
// again, tells nothing.
func (s OrderTimers) read(ctx restate.Context, seller uuid.UUID, sh domain.Shipment) (carrierRead, error) {
	r, err := restate.Run(ctx, func(rc restate.RunContext) (carrierRead, error) {
		p, readErr := s.Deals.d.Shipping.Track(rc, seller, carrier.Carrier(sh.Carrier), sh.TrackingCode)
		if errors.Is(readErr, carrier.ErrNotFound) {
			return carrierRead{Gone: true}, nil
		}
		if restatex.IsRefusal(readErr) {
			return carrierRead{}, nil
		}
		if readErr != nil {
			return carrierRead{}, fmt.Errorf("read %s: %w", sh.TrackingCode, readErr)
		}
		st, moves := p.Step.Reported()
		if !moves {
			return carrierRead{}, nil
		}
		return carrierRead{Status: domain.ShipmentStatus(st), At: p.StepAt, Fee: p.Fee, Gone: st == carrier.StatusLost}, nil
	})
	if err != nil {
		return carrierRead{}, fmt.Errorf("read the shipment at %s: %w", sh.Carrier, err)
	}
	return r, nil
}

// move sends ShipmentUpdates what the carrier read tells of sh.
func (s OrderTimers) move(ctx restate.Context, sh domain.Shipment, r carrierRead) error {
	if _, err := restate.Service[ShipmentResult](ctx, s.Deals.d.Prefix+shipmentUpdates, flowHandler).
		Request(ShipmentSignal{Carrier: sh.Carrier, TrackingCode: sh.TrackingCode, Status: r.Status, At: r.At, Fee: r.Fee}); err != nil {
		return fmt.Errorf("move the order: %w", err)
	}
	return nil
}

// readMoved reads sh from its carrier with the seller's account and, when
// the carrier reports a status that moves it to another or a new fee,
// moves the order through ShipmentUpdates. A status the carrier does not
// map, a code it lost, a parcel cancelled or lost before the pickup, or a
// refusal of shipping's moves nothing. moved tells whether the status
// moved; a new fee alone is not a move.
func (s OrderTimers) readMoved(ctx restate.Context, seller uuid.UUID, sh domain.Shipment) (moved bool, err error) {
	r, err := s.read(ctx, seller, sh)
	if err != nil {
		return false, err
	}
	moved = r.Status != "" && r.Status != sh.Status && (r.Status != domain.ShipmentLost || sh.PickedUpAt != nil)
	if !moved && !r.newFee(sh) {
		return false, nil
	}
	if err = s.move(ctx, sh, r); err != nil {
		return false, err
	}
	return moved, nil
}
