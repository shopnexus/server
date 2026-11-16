package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	supportdomain "github.com/shopnexus/server2/internal/support/domain"
	supportsvc "github.com/shopnexus/server2/internal/support/service"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

type OrderScreen struct {
	*BuyerOrderView

	Order       dealsvc.OrderView  `json:"order"`
	Deal        dealsvc.DealView   `json:"deal"`
	Counterpart chatsvc.PersonView `json:"counterpart"`
}

type BuyerOrderView struct {
	OwnReview trustsvc.OwnReview     `json:"own_review"`
	Ticket    *supportsvc.TicketView `json:"ticket"`
}

func (s *Screen) Order(ctx context.Context, account, order uuid.UUID) (OrderScreen, error) {
	o, dealID, err := s.deals.OrderFor(ctx, account, order)
	if err != nil {
		return OrderScreen{}, fmt.Errorf("read selected order: %w", err)
	}
	d, err := s.deals.Get(ctx, account, dealID)
	if err != nil {
		return OrderScreen{}, fmt.Errorf("read current deal: %w", err)
	}
	other := d.BuyerID
	if d.Role == dealdomain.Buyer {
		other = d.SellerID
	}
	person, err := s.person(ctx, other)
	if err != nil {
		return OrderScreen{}, err
	}
	out := OrderScreen{Order: o, Deal: d, Counterpart: person}
	if d.Role == dealdomain.Buyer {
		reviews, reviewErr := s.trust.OwnReviews(ctx, account, []uuid.UUID{order})
		if reviewErr != nil {
			return OrderScreen{}, fmt.Errorf("read own review: %w", reviewErr)
		}
		out.BuyerOrderView = &BuyerOrderView{OwnReview: reviews[order]}
		ticket, ticketErr := s.support.OfOrder(ctx, account, order)
		if ticketErr != nil && !errors.Is(ticketErr, supportdomain.ErrNotFound) {
			return OrderScreen{}, fmt.Errorf("read order ticket: %w", ticketErr)
		}
		if ticketErr == nil {
			out.Ticket = &ticket
		}
	}
	return out, nil
}

func (s *Screen) person(ctx context.Context, id uuid.UUID) (chatsvc.PersonView, error) {
	a, err := s.accounts.Get(ctx, id)
	if err != nil {
		return chatsvc.PersonView{}, fmt.Errorf("read person: %w", err)
	}
	return chatsvc.PersonFromAccount(id, a), nil
}
