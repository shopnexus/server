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
// picked up Waits.Pickup after the shipment, calls a linked shipment off at
// GHTK and gives the listing back.
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
		if sh := d.Order().Shipment; sh.Mode == domain.ModeLinked {
			if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
				return s.Deals.carrier.Cancel(rc, carrier.Carrier(sh.Carrier), sh.TrackingCode)
			}); err != nil {
				return fmt.Errorf("call off the shipment at GHTK: %w", err)
			}
		}
		return releaseStep(ctx, s.Deals.catalog, m.Listing.ID, ref.OrderID)
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
