package service

import (
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// dealCancelOrder is the DealCancelOrder service's name without the prefix.
const dealCancelOrder = "DealCancelOrder"

// DealCancelOrder cancels a pending order on either side's word; the deal
// negotiates again.
type DealCancelOrder struct{ Deals *Deals }

func (s DealCancelOrder) ServiceName() string { return s.Deals.prefix + dealCancelOrder }

// Run is UC-DEAL-06.
func (s DealCancelOrder) Run(ctx restate.Context, in FlowInput) (FlowResult, error) {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return FlowResult{}, err
	}
	// A retry of a cancel that landed answers as the first did.
	m, err := loadStep(ctx, s.Deals, in.Caller, in.DealID, in.Key)
	if err != nil || m.Refused != nil || m.Moved {
		return FlowResult{Refused: m.Refused}, err
	}
	d := domain.RestoreDeal(m.Deal)
	// E1: only the deal's current pending order cancels; a shipped or
	// historical one is domain.ErrMoveNotAllowed.
	if err = d.CancelPendingOrder(m.By, in.OrderID, at); err != nil {
		return FlowResult{Refused: apperr.From(err)}, nil
	}
	refused, err := saveStep(ctx, s.Deals.deals, &d, in.Key, at)
	if err != nil {
		return FlowResult{}, err
	}
	// A racing try of this cancel that saved first answers as it did.
	moved, err := keyMovedStep(ctx, s.Deals.deals, refused, in.DealID, m.By, in.Key)
	if err != nil || moved {
		return FlowResult{}, err
	}
	return FlowResult{Refused: refused}, nil
}
