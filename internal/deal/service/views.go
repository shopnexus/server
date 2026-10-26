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
	Order       *OrderView        `json:"order,omitempty"`
	Moves       []MoveView        `json:"moves"`
}

// OrderView is the Order schema in api/schemas/deal.yaml.
type OrderView struct {
	ID           uuid.UUID           `json:"id"`
	Code         string              `json:"code"`
	Status       domain.OrderStatus  `json:"status"`
	Price        int64               `json:"price"`
	Recipient    RecipientView       `json:"recipient"`
	Waybill      *domain.Waybill     `json:"waybill,omitempty"`
	CancelledBy  domain.Party        `json:"cancelled_by,omitempty"`
	CancelReason domain.CancelReason `json:"cancel_reason,omitempty"`
	RequestedAt  time.Time           `json:"requested_at"`
	ShippedAt    *time.Time          `json:"shipped_at,omitempty"`
	ClosedAt     *time.Time          `json:"closed_at,omitempty"`
}

// RecipientView is where the parcel goes, with the commune's and the
// province's names. Phone and Street are absent where the viewer may not
// read them.
type RecipientView struct {
	Name        string `json:"name"`
	Phone       string `json:"phone,omitempty"`
	Street      string `json:"street,omitempty"`
	CommuneCode string `json:"commune_code"`
	Commune     string `json:"commune"`
	Province    string `json:"province"`
}

// OrderItem is an order in a list, with its deal and listing.
type OrderItem struct {
	OrderView

	DealID  uuid.UUID  `json:"deal_id"`
	Listing ListingRef `json:"listing"`
}

// RecipientInput is a recipient as the buyer types it.
type RecipientInput struct {
	Name        string
	Phone       string
	Street      string
	CommuneCode string
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
	catalogdomain.StatusReserved:   domain.ListingReserved,
	catalogdomain.StatusSold:       domain.ListingGone,
	catalogdomain.StatusDeleted:    domain.ListingGone,
}

// facts is what the deal's rules read of its listing.
func facts(l catalogsvc.Summary) domain.Listing {
	return domain.Listing{Price: l.Price, AllowOffers: l.AllowOffers, Status: statusOf[l.Status], ReservedFor: l.ReservedFor}
}

// listingRef is the deal's listing as a view shows it.
func listingRef(l catalogsvc.Summary) ListingRef {
	return ListingRef{ID: l.ID, Title: l.Title, Price: l.Price, CoverThumbURL: l.CoverThumbURL, Status: l.Status}
}

// orderView shows o to viewer, with the recipient's commune at place.
func orderView(o domain.Order, viewer domain.Party, place catalogsvc.Location) OrderView {
	r := o.RecipientFor(viewer)
	return OrderView{
		ID: o.ID, Code: o.Code, Status: o.Status, Price: o.Price,
		Recipient: RecipientView{
			Name: r.Name, Phone: r.Phone, Street: r.Street, CommuneCode: r.CommuneCode,
			Commune: place.Commune.DisplayName, Province: place.Province.DisplayName,
		},
		Waybill: o.Waybill, CancelledBy: o.CancelledBy, CancelReason: o.CancelReason,
		RequestedAt: o.RequestedAt, ShippedAt: o.ShippedAt, ClosedAt: o.ClosedAt,
	}
}

// view builds the DealView of d for viewer, one of its sides, from its
// listing, its moves and the place of its order's recipient.
func view(viewer uuid.UUID, d domain.Deal, l catalogsvc.Summary, moves []domain.Move, place catalogsvc.Location) DealView {
	by, _ := d.PartyOf(viewer)
	v := DealView{
		ID: d.ID(), BuyerID: d.BuyerID(), SellerID: d.SellerID(),
		Listing: listingRef(l),
		Role:    by, Status: d.Status(), OpenPrice: d.OpenFor(facts(l)), AgreedAt: d.AgreedAt(),
		Moves: make([]MoveView, 0, len(moves)),
	}
	if d.Status() == domain.StatusAgreed || d.Status() == domain.StatusSold {
		v.AgreedPrice = new(d.AgreedPrice())
	}
	if o := d.Order(); o != nil {
		v.Order = new(orderView(*o, by, place))
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

// read builds d's view for viewer, reading its moves and its order's place.
func (s *Deals) read(ctx context.Context, viewer uuid.UUID, d domain.Deal, l catalogsvc.Summary) (DealView, error) {
	moves, err := s.deals.Moves(ctx, []uuid.UUID{d.ID()})
	if err != nil {
		return DealView{}, fmt.Errorf("read moves: %w", err)
	}
	place, err := s.placeOf(ctx, d.Order())
	if err != nil {
		return DealView{}, err
	}
	return view(viewer, d, l, moves[d.ID()], place), nil
}

// placeOf reads the commune and province of o's recipient; no order has no
// place.
func (s *Deals) placeOf(ctx context.Context, o *domain.Order) (catalogsvc.Location, error) {
	if o == nil {
		return catalogsvc.Location{}, nil
	}
	place, err := s.catalog.Place(ctx, o.Recipient.CommuneCode)
	if err != nil {
		return catalogsvc.Location{}, fmt.Errorf("read the recipient's commune: %w", err)
	}
	return place, nil
}
