package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
	supportdomain "github.com/shopnexus/server/internal/support/domain"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

type OrderScreen struct {
	*BuyerOrderView

	Order       dealsvc.OrderView     `json:"order"`
	Deal        dealsvc.DealView      `json:"deal"`
	Counterpart chatsvc.PersonView    `json:"counterpart"`
	OwnReview   trustsvc.OwnReview    `json:"own_review"`
	TheirReview *trustsvc.TheirReview `json:"their_review"`
	// Tracking is absent until the carrier reports something.
	Tracking *shippingsvc.Tracking `json:"tracking,omitempty"`
}

type BuyerOrderView struct {
	Ticket *supportsvc.TicketView `json:"ticket"`
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
	reviews, err := s.trust.OrderReviews(ctx, account, []uuid.UUID{order})
	if err != nil {
		return OrderScreen{}, fmt.Errorf("read the reviews: %w", err)
	}
	out := OrderScreen{Order: o, Deal: d, Counterpart: person, OwnReview: reviews[order].Own, TheirReview: reviews[order].Their}
	if sh := o.Shipment; sh != nil {
		t, trackErr := s.shipping.Tracking(ctx, string(sh.Carrier), sh.TrackingCode, d.Role == dealdomain.Seller)
		if trackErr != nil {
			return OrderScreen{}, fmt.Errorf("read the tracking: %w", trackErr)
		}
		if len(t.Events) > 0 {
			out.Tracking = &t
		}
	}
	if d.Role == dealdomain.Buyer {
		out.BuyerOrderView = &BuyerOrderView{}
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
