package service

import (
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// dealRequestCod is the DealRequestCod service's name without the prefix.
const dealRequestCod = "DealRequestCod"

// CodInput names the buyer, the deal, the price the buyer saw and the
// address the order goes to.
type CodInput struct {
	Caller    uuid.UUID `json:"caller"`
	DealID    uuid.UUID `json:"deal_id"`
	Price     int64     `json:"price"`
	AddressID uuid.UUID `json:"address_id"`
}

// buyerFacts is what the buyer's step journals: a copy of their address
// as the recipient, or account's refusal, or the seller's away mode.
type buyerFacts struct {
	Recipient domain.Recipient `json:"recipient"`
	Refused   *apperr.Error    `json:"refused,omitempty"`
}

// DealRequestCod makes the pending order of a deal at the price the buyer
// saw, agreeing it when it still negotiates, to a copy of
// one of the buyer's addresses, then schedules the seller's timeout.
type DealRequestCod struct{ Deals *Deals }

func (s DealRequestCod) ServiceName() string { return s.Deals.prefix + dealRequestCod }

// Run is UC-DEAL-03.
func (s DealRequestCod) Run(ctx restate.Context, in CodInput) (FlowResult, error) {
	at, err := restatex.Now(ctx, s.Deals.now)
	if err != nil {
		return FlowResult{}, err
	}
	m, err := loadStep(ctx, s.Deals, in.Caller, in.DealID)
	if err != nil || m.Refused != nil {
		return FlowResult{Refused: m.Refused}, err
	}
	// E1 · BR-MOD-02, and E2: someone else's address is
	// accountdomain.ErrAddressNotFound.
	b, err := restate.Run(ctx, func(rc restate.RunContext) (buyerFacts, error) {
		status, stepErr := s.Deals.accounts.Status(rc, in.Caller)
		if stepErr != nil {
			return buyerFacts{}, fmt.Errorf("read account status: %w", stepErr)
		}
		if r := status.Refusal(); r != nil {
			return buyerFacts{Refused: r}, nil //nolint:nilerr // A business refusal goes back as data.
		}
		// A new order waits on the seller, who must not be away.
		if m.By == domain.Buyer {
			away, awayErr := s.Deals.accounts.Away(rc, m.Deal.SellerID)
			if awayErr != nil {
				return buyerFacts{}, fmt.Errorf("read the seller's away mode: %w", awayErr)
			}
			if away {
				return buyerFacts{Refused: domain.ErrSellerAway}, nil
			}
		}
		a, stepErr := s.Deals.accounts.Address(rc, in.Caller, in.AddressID)
		if stepErr != nil {
			refused, faultErr := restatex.AsRefusal(stepErr)
			if faultErr != nil {
				return buyerFacts{}, fmt.Errorf("read the address: %w", faultErr)
			}
			return buyerFacts{Refused: refused.Refusal}, nil
		}
		return buyerFacts{Recipient: domain.Recipient{
			Name: a.Name, Phone: a.Phone, Street: a.Street, CommuneCode: a.CommuneCode, Kind: domain.AddressKind(a.Kind),
		}}, nil
	})
	if err != nil {
		return FlowResult{}, fmt.Errorf("read the buyer: %w", err)
	}
	if b.Refused != nil {
		return FlowResult{Refused: b.Refused}, nil //nolint:nilerr // A business refusal goes back as data.
	}
	// 2. The pending order; E4 and E5 are the domain's refusals, E3 the
	// store's, which counts open orders in the save (BR-DEAL-06).
	d := domain.RestoreDeal(m.Deal)
	order := restatex.NewID(ctx, at)
	if err = d.RequestCod(m.By, order, in.Price, b.Recipient, facts(m.Listing), at); err != nil {
		return FlowResult{Refused: apperr.From(err)}, nil
	}
	refused, err := saveStep(ctx, s.Deals.deals, &d, at)
	if err != nil || refused != nil {
		return FlowResult{Refused: refused}, err
	}
	// UC-DEAL-06 A1: the seller's timeout.
	restate.ServiceSend(ctx, s.Deals.prefix+orderTimers, "SellerTimeout").
		Send(OrderRef{DealID: in.DealID, OrderID: order}, restate.WithDelay(s.Deals.waits.Seller))
	return FlowResult{}, nil
}
