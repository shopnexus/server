package service

import (
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// dealConfirm is the DealConfirm service's name without the prefix.
const dealConfirm = "DealConfirm"

// DealConfirm completes a shipped order on its buyer's word and marks the
// listing sold. Restate retries the listing step until it is done.
type DealConfirm struct{ Deals *Deals }

func (s DealConfirm) ServiceName() string { return s.Deals.prefix + dealConfirm }

func (s DealConfirm) Run(ctx restate.Context, in FlowInput) (FlowResult, error) {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return FlowResult{}, err
	}
	m, err := loadStep(ctx, s.Deals, in.Caller, in.DealID)
	if err != nil || m.Refused != nil {
		return FlowResult{Refused: m.Refused}, err
	}
	d := domain.RestoreDeal(m.Deal)
	if err = d.ConfirmReceipt(m.By, in.OrderID, at); err != nil {
		return FlowResult{Refused: apperr.From(err)}, nil
	}
	refused, err := saveStep(ctx, s.Deals.deals, &d, restatex.NewID(ctx, at), restatex.NewID(ctx, at), at)
	if err != nil || refused != nil {
		return FlowResult{Refused: refused}, err
	}
	return FlowResult{}, sellStep(ctx, s.Deals.catalog, m.Listing.ID, in.OrderID)
}
