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
	m, err := loadStep(ctx, s.Deals, in.Caller, in.DealID)
	if err != nil || m.Refused != nil {
		return FlowResult{Refused: m.Refused}, err
	}
	d := domain.RestoreDeal(m.Deal)
	// E1: only the deal's current pending order cancels; a shipped or
	// historical one is domain.ErrMoveNotAllowed.
	if err = d.CancelPendingOrder(m.By, in.OrderID, at); err != nil {
		return FlowResult{Refused: apperr.From(err)}, nil
	}
	refused, err := saveStep(ctx, s.Deals.deals, &d, restatex.NewID(ctx, at), restatex.NewID(ctx, at), at)
	return FlowResult{Refused: refused}, err
}
