package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
)

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

// load reads the caller's deal and its listing for a move. binds refuses a
// suspended caller: an offer or an accept binds them, while a decline, a
// withdrawal or a cancel takes a price back.
func (s *Deals) load(ctx context.Context, caller, id uuid.UUID, binds bool) (moving, error) {
	d, by, err := s.own(ctx, caller, id)
	if err != nil {
		return moving{}, err
	}
	if binds {
		if err = s.refuseSuspended(ctx, caller); err != nil {
			return moving{}, err
		}
	}
	l, err := s.listing(ctx, d.ListingID())
	if err != nil {
		return moving{}, err
	}
	return moving{deal: d, by: by, listing: l, rules: facts(l), now: s.now()}, nil
}

// save writes the moved deal with deal.moved and returns it as the caller
// sees it. A deal the other side moved meanwhile is domain.ErrChanged.
func (s *Deals) save(ctx context.Context, caller uuid.UUID, m moving) (DealView, error) {
	err := s.deals.Save(ctx, &m.deal, uuid.NewV7(), moved(m.deal.ID(), m.now))
	if errors.Is(err, repository.ErrConflict) {
		return DealView{}, domain.ErrChanged
	}
	if err != nil {
		return DealView{}, fmt.Errorf("save deal: %w", err)
	}
	return s.read(ctx, caller, m.deal, m.listing)
}
