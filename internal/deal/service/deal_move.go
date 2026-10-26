package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
)

func (s *Deals) Offer(ctx context.Context, caller, id uuid.UUID, amount int64) (DealView, error) {
	return s.move(ctx, caller, id, true, func(d *domain.Deal, by domain.Party, l domain.Listing, now time.Time) error {
		return d.Offer(by, amount, l, now)
	})
}

func (s *Deals) Accept(ctx context.Context, caller, id uuid.UUID, price int64) (DealView, error) {
	return s.move(ctx, caller, id, true, func(d *domain.Deal, by domain.Party, l domain.Listing, now time.Time) error {
		return d.Accept(by, price, l, now)
	})
}

func (s *Deals) Decline(ctx context.Context, caller, id uuid.UUID) (DealView, error) {
	return s.move(ctx, caller, id, false, func(d *domain.Deal, by domain.Party, _ domain.Listing, now time.Time) error {
		return d.Decline(by, now)
	})
}

func (s *Deals) Withdraw(ctx context.Context, caller, id uuid.UUID) (DealView, error) {
	return s.move(ctx, caller, id, false, func(d *domain.Deal, by domain.Party, _ domain.Listing, now time.Time) error {
		return d.Withdraw(by, now)
	})
}

func (s *Deals) Cancel(ctx context.Context, caller, id uuid.UUID) (DealView, error) {
	return s.move(ctx, caller, id, false, func(d *domain.Deal, by domain.Party, _ domain.Listing, now time.Time) error {
		return d.Cancel(by, now)
	})
}

// move loads the caller's deal and its listing, applies change as the
// caller's side and saves the deal with deal.moved. commits refuses a
// suspended caller: an offer or an accept binds them, while a decline, a
// withdrawal or a cancel takes a price back.
func (s *Deals) move(ctx context.Context, caller, id uuid.UUID, commits bool,
	change func(d *domain.Deal, by domain.Party, l domain.Listing, now time.Time) error,
) (DealView, error) {
	d, by, err := s.own(ctx, caller, id)
	if err != nil {
		return DealView{}, err
	}
	if commits {
		if err = s.refuseSuspended(ctx, caller); err != nil {
			return DealView{}, err
		}
	}
	l, err := s.listing(ctx, d.ListingID())
	if err != nil {
		return DealView{}, err
	}
	now := s.now()
	if err = change(&d, by, facts(l), now); err != nil {
		return DealView{}, err
	}
	err = s.deals.Save(ctx, &d, uuid.NewV7(), moved(d.ID(), now))
	if errors.Is(err, repository.ErrConflict) {
		return DealView{}, domain.ErrChanged
	}
	if err != nil {
		return DealView{}, fmt.Errorf("save deal: %w", err)
	}
	return s.read(ctx, caller, d, l)
}
