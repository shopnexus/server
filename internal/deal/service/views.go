package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/deal/domain"
)

// DealView is the Deal schema in api/schemas/deal.yaml.
type DealView struct {
	ID          uuid.UUID         `json:"id"`
	BuyerID     uuid.UUID         `json:"buyer_id"`
	SellerID    uuid.UUID         `json:"seller_id"`
	Listing     ListingRef        `json:"listing"`
	Role        domain.Party      `json:"role"`
	Status      domain.Status     `json:"status"`
	OpenPrice   *domain.OpenPrice `json:"open_price,omitempty"`
	AgreedPrice *int64            `json:"agreed_price,omitempty"`
	AgreedAt    *time.Time        `json:"agreed_at,omitempty"`
	OfferRange  *domain.Range     `json:"offer_range,omitempty"`
	Moves       []MoveView        `json:"moves"`
}

// ListingRef is the deal's listing as it is now.
type ListingRef struct {
	ID            uuid.UUID            `json:"id"`
	Title         string               `json:"title"`
	Price         int64                `json:"price"`
	CoverThumbURL string               `json:"cover_thumb_url,omitempty"`
	Status        catalogdomain.Status `json:"status"`
}

// MoveView is one move of the thread; Amount is absent on a move without
// one.
type MoveView struct {
	Kind   domain.MoveKind `json:"kind"`
	By     domain.Party    `json:"by"`
	Amount *int64          `json:"amount,omitempty"`
	At     time.Time       `json:"at"`
}

// statusOf is how the deal's rules see each listing status.
var statusOf = map[catalogdomain.Status]domain.ListingStatus{
	catalogdomain.StatusActive:     domain.ListingActive,
	catalogdomain.StatusProcessing: domain.ListingPaused,
	catalogdomain.StatusRejected:   domain.ListingPaused,
	catalogdomain.StatusHidden:     domain.ListingHidden,
	catalogdomain.StatusSold:       domain.ListingGone,
	catalogdomain.StatusDeleted:    domain.ListingGone,
}

// facts is what the deal's rules read of its listing.
func facts(l catalogsvc.Summary) domain.Listing {
	return domain.Listing{Price: l.Price, AllowOffers: l.AllowOffers, Status: statusOf[l.Status]}
}

// view builds the DealView of d for viewer, one of its sides, from its
// listing and moves.
func view(viewer uuid.UUID, d domain.Deal, l catalogsvc.Summary, moves []domain.Move) DealView {
	by, _ := d.PartyOf(viewer)
	v := DealView{
		ID: d.ID(), BuyerID: d.BuyerID(), SellerID: d.SellerID(),
		Listing: ListingRef{ID: l.ID, Title: l.Title, Price: l.Price, CoverThumbURL: l.CoverThumbURL, Status: l.Status},
		Role:    by, Status: d.Status(), OpenPrice: d.OpenFor(facts(l)), AgreedAt: d.AgreedAt(),
		Moves: make([]MoveView, 0, len(moves)),
	}
	if d.Status() == domain.StatusAgreed {
		v.AgreedPrice = new(d.AgreedPrice())
	}
	if r, ok := d.OfferRange(by, facts(l)); ok {
		v.OfferRange = &r
	}
	for _, m := range moves {
		mv := MoveView{Kind: m.Kind, By: m.By, At: m.At}
		if m.Amount != 0 {
			mv.Amount = new(m.Amount)
		}
		v.Moves = append(v.Moves, mv)
	}
	return v
}

// read builds d's view for viewer, reading its moves.
func (s *Deals) read(ctx context.Context, viewer uuid.UUID, d domain.Deal, l catalogsvc.Summary) (DealView, error) {
	moves, err := s.deals.Moves(ctx, []uuid.UUID{d.ID()})
	if err != nil {
		return DealView{}, fmt.Errorf("read moves: %w", err)
	}
	return view(viewer, d, l, moves[d.ID()]), nil
}
