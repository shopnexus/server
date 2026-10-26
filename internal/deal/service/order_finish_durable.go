package service

import (
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// dealFinish is the DealFinish service's name without the prefix.
const dealFinish = "DealFinish"

// DealFinish ends a shipped order as in.Outcome says: delivered marks its
// listing sold, returned puts it back on sale. Restate retries the listing
// step until it is done.
type DealFinish struct{ Deals *Deals }

func (s DealFinish) ServiceName() string { return s.Deals.prefix + dealFinish }

func (s DealFinish) Run(ctx restate.Context, in FlowInput) (FlowResult, error) {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return FlowResult{}, err
	}
	m, err := loadStep(ctx, s.Deals, in.Caller, in.DealID)
	if err != nil || m.Refused != nil {
		return FlowResult{Refused: m.Refused}, err
	}
	d := domain.RestoreDeal(m.Deal)
	if in.Outcome == domain.OrderDelivered {
		err = d.Deliver(m.By, at)
	} else {
		err = d.Return(m.By, at)
	}
	if err != nil {
		return FlowResult{Refused: apperr.From(err)}, nil
	}
	refused, err := saveStep(ctx, s.Deals.deals, &d, restatex.NewID(ctx, at), restatex.NewID(ctx, at), at)
	if err != nil || refused != nil {
		return FlowResult{Refused: refused}, err
	}
	if in.Outcome == domain.OrderDelivered {
		err = sellStep(ctx, s.Deals.catalog, m.Listing.ID, d.Order().ID)
	} else {
		err = releaseStep(ctx, s.Deals.catalog, m.Listing.ID, d.Order().ID)
	}
	return FlowResult{}, err
}
