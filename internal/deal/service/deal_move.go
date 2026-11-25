package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
)

// Offer is UC-DEAL-02, steps 2 and 3.
func (s *Deals) Offer(ctx context.Context, caller, id uuid.UUID, amount int64) (DealView, error) {
	m, err := s.load(ctx, caller, id, true)
	if err != nil {
		return DealView{}, err
	}
	if err = m.deal.Offer(m.by, amount, m.rules, m.now); err != nil {
		return DealView{}, err
	}
	return s.save(ctx, caller, m)
}

// Accept is UC-DEAL-02, step 4.
func (s *Deals) Accept(ctx context.Context, caller, id uuid.UUID, price int64) (DealView, error) {
	m, err := s.load(ctx, caller, id, true)
	if err != nil {
		return DealView{}, err
	}
	if err = m.deal.Accept(m.by, price, m.rules, m.now); err != nil {
		return DealView{}, err
	}
	return s.save(ctx, caller, m)
}

// Decline is UC-DEAL-02, step 3.
func (s *Deals) Decline(ctx context.Context, caller, id uuid.UUID) (DealView, error) {
	m, err := s.load(ctx, caller, id, false)
	if err != nil {
		return DealView{}, err
	}
	if err = m.deal.Decline(m.by, m.now); err != nil {
		return DealView{}, err
	}
	return s.save(ctx, caller, m)
}

// Withdraw is UC-DEAL-02, step 3.
func (s *Deals) Withdraw(ctx context.Context, caller, id uuid.UUID) (DealView, error) {
	m, err := s.load(ctx, caller, id, false)
	if err != nil {
		return DealView{}, err
	}
	if err = m.deal.Withdraw(m.by, m.now); err != nil {
		return DealView{}, err
	}
	return s.save(ctx, caller, m)
}

// Cancel is UC-DEAL-02 A3 on an agreed deal and UC-DEAL-06 on a pending
// order.
func (s *Deals) Cancel(ctx context.Context, caller, id uuid.UUID) (DealView, error) {
	m, err := s.load(ctx, caller, id, false)
	if err != nil {
		return DealView{}, err
	}
	if err = m.deal.Cancel(m.by, m.now); err != nil {
		return DealView{}, err
	}
	return s.save(ctx, caller, m)
}

// moving is a deal loaded for a move: the caller's side of it, its listing
// as catalog has it and as the rules read it, and the time of the move.
type moving struct {
	deal    domain.Deal
	by      domain.Party
	listing catalogsvc.Summary
	rules   domain.Listing
	now     time.Time
}

// load reads the caller's deal and its listing for a move, in UC-DEAL-02
// and UC-DEAL-03. binds refuses a suspended caller: an offer, an accept or
// a COD request binds them, while a decline, a withdrawal or a cancel takes
// a price back. A buyer's binding move also binds the seller, so it is
// refused while the seller is away.
func (s *Deals) load(ctx context.Context, caller, id uuid.UUID, binds bool) (moving, error) {
	d, by, err := s.own(ctx, caller, id)
	if err != nil {
		return moving{}, err
	}
	// E1 · BR-MOD-02
	if binds {
		status, readErr := s.d.Accounts.Status(ctx, caller)
		if readErr != nil {
			return moving{}, fmt.Errorf("read account status: %w", readErr)
		}
		if r := status.Refusal(); r != nil {
			return moving{}, r
		}
	}
	if binds && by == domain.Buyer {
		away, readErr := s.d.Accounts.Away(ctx, d.SellerID())
		if readErr != nil {
			return moving{}, fmt.Errorf("read the seller's away mode: %w", readErr)
		}
		if away {
			return moving{}, domain.ErrSellerAway
		}
	}
	l, err := s.listing(ctx, d.ListingID())
	if err != nil {
		return moving{}, err
	}
	return moving{deal: d, by: by, listing: l, rules: facts(l), now: s.d.Now()}, nil
}

// save writes the moved deal with deal.moved and returns it as the caller
// sees it. None of its moves makes an order, so nothing the store sets
// needs a read back. A deal the other side moved meanwhile is
// domain.ErrChanged (UC-DEAL-02 E8, NFR-CONC-01).
func (s *Deals) save(ctx context.Context, caller uuid.UUID, m moving) (DealView, error) {
	err := s.d.Deals.Save(ctx, &m.deal, uuid.NewV7(), uuid.Nil(), moved(m.deal.ID(), m.now))
	if errors.Is(err, repository.ErrConflict) {
		return DealView{}, domain.ErrChanged
	}
	if err != nil {
		return DealView{}, fmt.Errorf("save deal: %w", err)
	}
	return s.read(ctx, caller, m.deal, m.listing)
}
