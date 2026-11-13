package service

import (
	"errors"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/carrier"
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

func (s OrderTimers) ServiceName() string { return s.Deals.prefix + orderTimers }

// SellerTimeout cancels a pending order whose seller made no shipment
// within Waits.Seller. A pending order holds no listing.
func (s OrderTimers) SellerTimeout(ctx restate.Context, ref OrderRef) error {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return err
	}
	for {
		m, loadErr := systemLoadStep(ctx, s.Deals, ref.DealID)
		if loadErr != nil {
			return loadErr
		}
		d := domain.RestoreDeal(m.Deal)
		if errors.Is(d.SellerTimeout(ref.OrderID, at), domain.ErrMoveNotAllowed) {
			return nil // the order moved on
		}
		refused, saveErr := saveStep(ctx, s.Deals.deals, &d, restatex.NewID(ctx, at), restatex.NewID(ctx, at), at)
		if saveErr != nil {
			return saveErr
		}
		if refused == nil {
			return nil
		}
	}
}

// PickupTimeout cancels a shipped order whose parcel the carrier had not
// picked up Waits.Pickup after the shipment, gives the listing back, then
// calls a linked shipment off at its carrier. A carrier that has the
// parcel already, or no longer knows it, leaves the order cancelled all
// the same.
func (s OrderTimers) PickupTimeout(ctx restate.Context, ref OrderRef) error {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return err
	}
	for {
		m, loadErr := systemLoadStep(ctx, s.Deals, ref.DealID)
		if loadErr != nil {
			return loadErr
		}
		d := domain.RestoreDeal(m.Deal)
		if errors.Is(d.PickupTimeout(ref.OrderID, at), domain.ErrMoveNotAllowed) {
			return nil // the order moved on
		}
		refused, saveErr := saveStep(ctx, s.Deals.deals, &d, restatex.NewID(ctx, at), restatex.NewID(ctx, at), at)
		if saveErr != nil {
			return saveErr
		}
		if refused != nil {
			continue
		}
		if err = releaseStep(ctx, s.Deals.catalog, m.Listing.ID, ref.OrderID); err != nil {
			return err
		}
		if sh := d.Order().Shipment; sh.Mode == domain.ModeLinked {
			if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
				callErr := s.Deals.shipping.Cancel(rc, m.Deal.SellerID, carrier.Carrier(sh.Carrier), sh.TrackingCode)
				if errors.Is(callErr, carrier.ErrNotCancellable) || errors.Is(callErr, carrier.ErrNotFound) || refusedBy(callErr) {
					return nil // the order is cancelled either way
				}
				if callErr != nil {
					return fmt.Errorf("cancel %s: %w", sh.TrackingCode, callErr)
				}
				return nil
			}); err != nil {
				return fmt.Errorf("call off the shipment at %s: %w", sh.Carrier, err)
			}
		}
		return nil
	}
}

// AutoComplete completes a shipped order Waits.Complete after its carrier
// reported the parcel delivered, unless the buyer confirmed first, and
// marks the listing sold.
func (s OrderTimers) AutoComplete(ctx restate.Context, ref OrderRef) error {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return err
	}
	for {
		m, loadErr := systemLoadStep(ctx, s.Deals, ref.DealID)
		if loadErr != nil {
			return loadErr
		}
		d := domain.RestoreDeal(m.Deal)
		if errors.Is(d.AutoComplete(ref.OrderID, at), domain.ErrMoveNotAllowed) {
			return nil // the order moved on
		}
		refused, saveErr := saveStep(ctx, s.Deals.deals, &d, restatex.NewID(ctx, at), restatex.NewID(ctx, at), at)
		if saveErr != nil {
			return saveErr
		}
		if refused == nil {
			return sellStep(ctx, s.Deals.catalog, m.Listing.ID, ref.OrderID)
		}
	}
}

// Poll reads a shipped order's shipment from its carrier with the seller's
// account and moves the order as a report would, then reads it again
// Waits.Poll later while the order stays shipped. It is the safety net
// under the carriers' reports: a report that came first leaves it nothing
// to change, and a code the carrier lost or a carrier unlinked waits for
// the next read.
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
	sh := *o.Shipment
	read, err := restate.Run(ctx, func(rc restate.RunContext) (domain.ShipmentStatus, error) {
		p, readErr := s.Deals.shipping.Lookup(rc, m.Deal.SellerID, carrier.Carrier(sh.Carrier), sh.TrackingCode)
		if errors.Is(readErr, carrier.ErrNotFound) || refusedBy(readErr) {
			return "", nil
		}
		if readErr != nil {
			return "", fmt.Errorf("read %s: %w", sh.TrackingCode, readErr)
		}
		return domain.ShipmentStatus(p.Status), nil
	})
	if err != nil {
		return fmt.Errorf("read the shipment at %s: %w", sh.Carrier, err)
	}
	if read != "" && read != sh.Status {
		if _, err = restate.Service[ShipmentResult](ctx, s.Deals.prefix+shipmentUpdates, flowHandler).
			Request(ShipmentSignal{TrackingCode: sh.TrackingCode, Status: read}); err != nil {
			return fmt.Errorf("move the order: %w", err)
		}
	}
	restate.ServiceSend(ctx, s.ServiceName(), "Poll").Send(ref, restate.WithDelay(s.Deals.waits.Poll))
	return nil
}
