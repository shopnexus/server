package service

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// dealShip is the DealShip service's name without the prefix.
const dealShip = "DealShip"

// DealShip records the waybill of a requested order. It holds the listing
// first, so a listing no longer on sale refuses before anything is
// written, and gives it back when the order's save loses to anything but
// another shipment of the same order. Once shipped, it cancels the
// listing's other requested orders.
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
	if err = d.Ship(m.By, *in.Waybill, facts(m.Listing), at); err != nil {
		return FlowResult{Refused: apperr.From(err)}, nil
	}
	order := d.Order().ID
	refused, err := reserveStep(ctx, s.Deals.catalog, m.Listing.ID, order)
	if err != nil || refused != nil {
		return FlowResult{Refused: refused}, err
	}
	refused, err = saveStep(ctx, s.Deals.deals, &d, restatex.NewID(ctx, at), restatex.NewID(ctx, at), at)
	if err != nil {
		return FlowResult{}, err
	}
	if refused == nil {
		// The listing's other requested orders end with sent_to_another.
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
			return s.Deals.ListingChanged(rc, m.Listing.ID)
		}); err != nil {
			return FlowResult{}, fmt.Errorf("cancel the other requests: %w", err)
		}
		return FlowResult{}, nil
	}
	// The save lost a race. Another try that shipped this order keeps the
	// listing; anything else, a cancel above all, gives it back.
	shipped, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		cur, stepErr := s.Deals.deals.Get(rc, in.DealID)
		if stepErr != nil {
			return false, fmt.Errorf("read the deal: %w", stepErr)
		}
		o := cur.Order()
		return o != nil && o.ID == order && o.Status == domain.OrderShipped, nil
	})
	if err != nil {
		return FlowResult{}, fmt.Errorf("check the order: %w", err)
	}
	if shipped {
		return FlowResult{}, nil
	}
	if err = releaseStep(ctx, s.Deals.catalog, m.Listing.ID, order); err != nil {
		return FlowResult{}, err
	}
	return FlowResult{Refused: refused}, nil
}
