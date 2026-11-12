package service

import (
	"errors"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/carrier"
)

// dealShip is the DealShip service's name without the prefix.
const dealShip = "DealShip"

// madeShipment is what the step that makes a linked shipment journals:
// GHTK's answer, or the refusal of a stack without a carrier.
type madeShipment struct {
	Created carrier.Created `json:"created"`
	Refused *apperr.Error   `json:"refused,omitempty"`
}

// DealShip ships a pending order. A manual shipment is checked whole
// before anything is held; a linked one is made at GHTK once the listing
// is held, and a stack without a carrier gives the listing back. It gives the listing back, and calls off a GHTK shipment the
// order did not take, when the order's save loses to anything but another
// shipment of the same order. Once shipped, it cancels the listing's other
// pending orders.
type DealShip struct{ Deals *Deals }

func (s DealShip) ServiceName() string { return s.Deals.prefix + dealShip }

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
	refused, err := reserveStep(ctx, s.Deals.catalog, m.Listing.ID, in.OrderID)
	if err != nil || refused != nil {
		return FlowResult{Refused: refused}, err
	}
	if in.Linked != nil {
		o := d.Order()
		made, createErr := restate.Run(ctx, func(rc restate.RunContext) (madeShipment, error) {
			c, stepErr := s.Deals.carrier.Create(rc, carrier.CreateInput{
				OrderID: o.ID, COD: o.Price, WeightGrams: in.Linked.WeightGrams,
				Service: string(in.Linked.Service), Pickup: string(in.Linked.Pickup),
				Recipient: carrier.Recipient{
					Name: o.Recipient.Name, Phone: o.Recipient.Phone, Street: o.Recipient.Street,
					CommuneCode: o.Recipient.CommuneCode,
				},
			})
			if errors.Is(stepErr, carrier.ErrUnavailable) {
				return madeShipment{Refused: domain.ErrCarrierUnavailable}, nil
			}
			if stepErr != nil {
				return madeShipment{}, fmt.Errorf("create: %w", stepErr)
			}
			return madeShipment{Created: c}, nil
		})
		if createErr != nil {
			return FlowResult{}, fmt.Errorf("create the shipment at GHTK: %w", createErr)
		}
		// A stack without a carrier makes no shipment: the listing goes back.
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
			Mode: domain.ModeLinked, Carrier: domain.CarrierGHTK, TrackingCode: created.TrackingCode,
			Fee: &created.Fee, Status: domain.ShipmentPendingPickup,
		}, facts(m.Listing), at); err != nil {
			return FlowResult{}, fmt.Errorf("ship the order: %w", err)
		}
	}
	refused, err = saveStep(ctx, s.Deals.deals, &d, restatex.NewID(ctx, at), restatex.NewID(ctx, at), at)
	if err != nil {
		return FlowResult{}, err
	}
	if refused == nil {
		// The listing's other pending orders end with shipped_to_another.
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
			return s.Deals.ListingChanged(rc, m.Listing.ID)
		}); err != nil {
			return FlowResult{}, fmt.Errorf("cancel the other orders: %w", err)
		}
		return FlowResult{}, nil
	}
	// The save lost a race. Another try that shipped this order keeps the
	// listing; anything else, a cancel above all, gives it back.
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
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
			return s.Deals.carrier.Cancel(rc, carrier.Carrier(mine.Carrier), mine.TrackingCode)
		}); err != nil {
			return FlowResult{}, fmt.Errorf("call off the shipment at GHTK: %w", err)
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
