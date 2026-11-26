package service

import (
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// dealConfirm is the DealConfirm service's name without the prefix.
const dealConfirm = "DealConfirm"

// DealConfirm completes a shipped order on its buyer's word and marks the
// listing sold. Restate retries the listing step until it is done.
type DealConfirm struct{ Deals *Deals }

func (s DealConfirm) ServiceName() string { return s.Deals.d.Prefix + dealConfirm }

// Run is UC-DEAL-05, step 4, and A2.
func (s DealConfirm) Run(ctx restate.Context, in FlowInput) (FlowResult, error) {
	at, err := restatex.Now(ctx, s.Deals.d.Now)
	if err != nil {
		return FlowResult{}, err
	}
	// A retry of a confirm that landed answers as the first did.
	m, err := loadStep(ctx, s.Deals, in.Caller, in.DealID, in.Key)
	if err != nil || m.Refused != nil || m.Moved {
		return FlowResult{Refused: m.Refused}, err
	}
	d := domain.RestoreDeal(m.Deal)
	// E3: only the buyer of a shipped order confirms it.
	if err = d.ConfirmReceipt(m.By, in.OrderID, at); err != nil {
		return FlowResult{Refused: apperr.From(err)}, nil
	}
	refused, err := saveStep(ctx, s.Deals.d.Deals, &d, in.Key, at)
	if err != nil {
		return FlowResult{}, err
	}
	if refused != nil {
		// A racing try of this confirm that saved first sells the listing.
		moved, movedErr := keyMovedStep(ctx, s.Deals.d.Deals, refused, in.DealID, m.By, in.Key)
		if movedErr != nil || moved {
			return FlowResult{}, movedErr
		}
		return FlowResult{Refused: refused}, nil
	}
	return FlowResult{}, sellStep(ctx, s.Deals.d.Catalog, m.Listing.ID, in.OrderID, at)
}
