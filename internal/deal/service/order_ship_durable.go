package service

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/carrier"
)

// dealShip is the DealShip service's name without the prefix.
const dealShip = "DealShip"

// madeShipment is what the step that makes a linked shipment journals: the
// carrier's answer, or shipping's refusal (no carrier in the stack, the
// carrier not linked, an address the carrier does not know).
type madeShipment struct {
	Created carrier.Created `json:"created"`
	Refused *apperr.Error   `json:"refused,omitempty"`
}

type resolvedPickup struct {
	Pickup  domain.ShipmentPickup `json:"pickup"`
	Refused *apperr.Error         `json:"refused,omitempty"`
}

// DealShip ships a pending order. A manual shipment is checked whole
// before anything is held; a linked one is made on the seller's account at
// its carrier once the listing is held, and a refusal of shipping's gives
// the listing back. It gives the listing back, and calls off a linked
// shipment the order did not take, when the order's save loses to anything
// but another shipment of the same order. Once shipped, it cancels the
// listing's other pending orders. It schedules the pickup timeout, the
// first poll of the shipment, and the completion of a parcel already
// delivered.
type DealShip struct{ Deals *Deals }

func (s DealShip) ServiceName() string { return s.Deals.prefix + dealShip }

// Run is UC-DEAL-04, steps 2 to 5 · ADR-0001 · ADR-0002.
func (s DealShip) Run(ctx restate.Context, in FlowInput) (FlowResult, error) {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return FlowResult{}, err
	}
	m, err := loadStep(ctx, s.Deals, in.Caller, in.DealID)
	if err != nil || m.Refused != nil {
		return FlowResult{Refused: m.Refused}, err
	}
	d := domain.RestoreDeal(m.Deal)
	if in.Manual != nil {
		err = d.Ship(m.By, in.OrderID, *in.Manual, facts(m.Listing), at)
	} else {
		err = d.Shippable(m.By, in.OrderID, facts(m.Listing))
	}
	if err != nil {
		return FlowResult{Refused: apperr.From(err)}, nil
	}
	var pickup *domain.ShipmentPickup
	if in.Linked != nil {
		pickup = in.Linked.From
		if in.SavedPickup {
			resolved, pickupErr := restate.Run(ctx, func(rc restate.RunContext) (resolvedPickup, error) {
				p, stepErr := s.Deals.shipping.SavedPickup(rc, in.Caller)
				if restatex.IsRefusal(stepErr) {
					return resolvedPickup{Refused: apperr.From(stepErr)}, nil
				}
				if stepErr != nil {
					return resolvedPickup{}, fmt.Errorf("read saved pickup: %w", stepErr)
				}
				return resolvedPickup{Pickup: domain.ShipmentPickup{Name: p.Name, Phone: p.Phone, Street: p.Street, CommuneCode: p.CommuneCode}}, nil
			})
			if pickupErr != nil {
				return FlowResult{}, fmt.Errorf("resolve saved pickup: %w", pickupErr)
			}
			if resolved.Refused != nil {
				return FlowResult{Refused: resolved.Refused}, nil //nolint:nilerr // A business refusal goes back as data.
			}
			pickup = &resolved.Pickup
		}
	}
	// 2. Hold the listing for the order; E5 when it is not on sale.
	refused, err := reserveStep(ctx, s.Deals.catalog, m.Listing.ID, in.OrderID)
	if err != nil || refused != nil {
		return FlowResult{Refused: refused}, err
	}
	// 3. Make the shipment on the seller's own account at the carrier.
	if in.Linked != nil {
		o, draft := d.Order(), *in.Linked
		draft.From = pickup
		made, createErr := restate.Run(ctx, func(rc restate.RunContext) (madeShipment, error) {
			r := o.Recipient
			to, stepErr := s.Deals.place(rc, r.Name, r.Phone, r.Street, r.CommuneCode)
			if stepErr != nil {
				return madeShipment{}, stepErr
			}
			from := carrier.Pickup{}
			if p := draft.From; p != nil && p.PointID != "" {
				from.PointID = p.PointID
			} else if p != nil {
				place, placeErr := s.Deals.place(rc, p.Name, p.Phone, p.Street, p.CommuneCode)
				if placeErr != nil {
					return madeShipment{}, placeErr
				}
				from.Place = &place
			}
			c, stepErr := s.Deals.shipping.Create(rc, in.Caller, carrier.Carrier(draft.Carrier), carrier.CreateInput{
				OrderCode: o.Code, Recipient: to, Pickup: from, Item: m.Listing.Title, COD: o.Price,
				WeightGrams: draft.WeightGrams, Service: string(draft.Service), Option: string(draft.Pickup),
				Note: deliveryNotes[r.Kind],
			})
			if stepErr = shippingError(stepErr, domain.ErrCarrierAddressUnknown); restatex.IsRefusal(stepErr) {
				return madeShipment{Refused: apperr.From(stepErr)}, nil
			}
			if stepErr != nil {
				return madeShipment{}, fmt.Errorf("create: %w", stepErr)
			}
			return madeShipment{Created: c}, nil
		})
		if createErr != nil {
			return FlowResult{}, fmt.Errorf("create the shipment at %s: %w", draft.Carrier, createErr)
		}
		// E4: a refused shipment is not made, and the listing goes back.
		if made.Refused != nil {
			if err = releaseStep(ctx, s.Deals.catalog, m.Listing.ID, in.OrderID); err != nil {
				return FlowResult{}, err
			}
			return FlowResult{Refused: made.Refused}, nil
		}
		created := made.Created
		// Shippable passed on this deal and listing, and a new shipment
		// waits for pickup, so Ship takes it.
		if err = d.Ship(m.By, in.OrderID, domain.Shipment{
			Mode: domain.ModeLinked, Carrier: draft.Carrier, TrackingCode: created.TrackingCode,
			Fee: &created.Fee, Status: domain.ShipmentPendingPickup, Pickup: draft.From,
		}, facts(m.Listing), at); err != nil {
			return FlowResult{}, fmt.Errorf("ship the order: %w", err)
		}
	}
	// 4. The order is shipped.
	refused, err = saveStep(ctx, s.Deals.deals, &d, at)
	if err != nil {
		return FlowResult{}, err
	}
	if refused == nil {
		// The listing's other pending orders end with shipped_to_another
		// (BR-DEAL-04).
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
			return s.Deals.ListingChanged(rc, m.Listing.ID)
		}); err != nil {
			return FlowResult{}, fmt.Errorf("cancel the other orders: %w", err)
		}
		// 5. Wait for the pickup and follow the shipment.
		ref := OrderRef{DealID: in.DealID, OrderID: in.OrderID}
		restate.ServiceSend(ctx, s.Deals.prefix+orderTimers, "PickupTimeout").
			Send(ref, restate.WithDelay(s.Deals.waits.Pickup))
		restate.ServiceSend(ctx, s.Deals.prefix+orderTimers, "Poll").
			Send(ref, restate.WithDelay(s.Deals.waits.Poll))
		// A2
		if d.Order().Shipment.Status == domain.ShipmentDelivered {
			restate.ServiceSend(ctx, s.Deals.prefix+orderTimers, "AutoComplete").
				Send(ref, restate.WithDelay(s.Deals.waits.Complete))
		}
		return FlowResult{}, nil
	}
	// E7 · NFR-CONC-01: the save lost a race. Another try that shipped this
	// order keeps the listing; anything else, a cancel above all, gives it
	// back.
	cur, err := restate.Run(ctx, func(rc restate.RunContext) (domain.Order, error) {
		got, stepErr := s.Deals.deals.Get(rc, in.DealID)
		if stepErr != nil {
			return domain.Order{}, fmt.Errorf("read the deal: %w", stepErr)
		}
		if o := got.Order(); o != nil {
			return *o, nil
		}
		return domain.Order{}, nil
	})
	if err != nil {
		return FlowResult{}, fmt.Errorf("check the order: %w", err)
	}
	mine := d.Order().Shipment
	shipped := cur.ID == in.OrderID && cur.Status == domain.OrderShipped
	if mine.Mode == domain.ModeLinked && (!shipped || cur.Shipment == nil || cur.Shipment.TrackingCode != mine.TrackingCode) {
		if err = callOffStep(ctx, s.Deals.shipping, s.Deals.log, in.Caller, *mine); err != nil {
			return FlowResult{}, err
		}
	}
	if shipped {
		return FlowResult{}, nil
	}
	if err = releaseStep(ctx, s.Deals.catalog, m.Listing.ID, in.OrderID); err != nil {
		return FlowResult{}, err
	}
	return FlowResult{Refused: refused}, nil
}
