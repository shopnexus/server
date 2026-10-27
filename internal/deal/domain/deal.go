// Package domain holds module deal's entity: a buyer and a seller agreeing
// a price on one listing, move by move.
package domain

import (
	"time"
	"uuid"
)

// Step is the unit of every offer and counter, in đồng.
const Step = 1_000

type Status string

const (
	StatusNegotiating Status = "negotiating"
	StatusAgreed      Status = "agreed"
	// StatusSold is a deal whose order was delivered; it takes no move.
	StatusSold Status = "sold"
)

// Party is who made a move. System is the server, following a change of
// the listing.
type Party string

const (
	Buyer  Party = "buyer"
	Seller Party = "seller"
	System Party = "system"
)

type MoveKind string

const (
	MoveOffer          MoveKind = "offer"
	MoveCounter        MoveKind = "counter"
	MoveAccept         MoveKind = "accept"
	MoveDecline        MoveKind = "decline"
	MoveWithdraw       MoveKind = "withdraw"
	MoveCancel         MoveKind = "cancel"
	MovePriceChanged   MoveKind = "price_changed"
	MoveOffersClosed   MoveKind = "offers_closed"
	MoveListingClosed  MoveKind = "listing_closed"
	MoveCodRequested   MoveKind = "cod_requested"
	MoveShipped        MoveKind = "shipped"
	MoveDelivered      MoveKind = "delivered"
	MoveReturned       MoveKind = "returned"
	MoveOrderCancelled MoveKind = "order_cancelled"
)

// Move is one step of a deal's thread. Amount is 0 on a move without one;
// on price_changed it is the new listed price, on cod_requested the price,
// on shipped the fee.
type Move struct {
	ID     uuid.UUID
	Kind   MoveKind
	By     Party
	Amount int64
	At     time.Time
}

// OpenPrice is the offer or counter waiting for the other side.
type OpenPrice struct {
	By     Party     `json:"by"`
	Amount int64     `json:"amount"`
	At     time.Time `json:"at"`
}

// ListingStatus is what the rules see of a listing's status.
type ListingStatus string

const (
	ListingActive ListingStatus = "active"
	// ListingPaused is a listing in processing or rejected after an edit
	// of its photos. It is not closed, and takes no new price.
	ListingPaused ListingStatus = "paused"
	ListingHidden ListingStatus = "hidden"
	// ListingReserved is a listing held for the order ReservedFor. It takes
	// no new price and keeps the open ones.
	ListingReserved ListingStatus = "reserved"
	// ListingGone is a listing sold or deleted.
	ListingGone ListingStatus = "gone"
)

// Listing is what the rules read of a deal's listing, as it is now.
type Listing struct {
	Price       int64
	AllowOffers bool
	Status      ListingStatus
	ReservedFor uuid.UUID
}

// Range is the amounts an offer may take, both ends included.
type Range struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

// Deal is a buyer's negotiation with a seller over one listing. It holds
// at most one open price; agreeing reserves nothing.
type Deal struct {
	id          uuid.UUID
	listingID   uuid.UUID
	buyerID     uuid.UUID
	sellerID    uuid.UUID
	status      Status
	open        *OpenPrice
	listedPrice int64
	agreedPrice int64
	agreedAt    *time.Time
	order       *Order
	writeID     uuid.UUID
	createdAt   time.Time
	updatedAt   time.Time
	pending     []Move
}

// Open starts the buyer's deal on a listing, negotiating with nothing
// open. The first write ID is the id.
func Open(id, listingID, buyer, seller uuid.UUID, now time.Time) Deal {
	return Deal{
		id: id, listingID: listingID, buyerID: buyer, sellerID: seller, status: StatusNegotiating,
		writeID: id, createdAt: now, updatedAt: now,
	}
}

// PartyOf returns account's side of the deal; false for anyone else.
func (d *Deal) PartyOf(account uuid.UUID) (Party, bool) {
	switch account {
	case d.buyerID:
		return Buyer, true
	case d.sellerID:
		return Seller, true
	}
	return "", false
}

// OfferRange is the amounts by's next offer may take: the buyer's with
// nothing open or under the seller's counter, the seller's counter over
// the buyer's offer. An open price the listing has voided counts as none.
// False when by may not offer now or no amount fits.
func (d *Deal) OfferRange(by Party, l Listing) (Range, bool) {
	if d.status != StatusNegotiating || l.Status != ListingActive {
		return Range{}, false
	}
	open := d.live(l)
	var r Range
	switch {
	case by == Buyer && l.AllowOffers && (open == nil || open.By == Seller):
		ceiling := l.Price
		if open != nil {
			ceiling = open.Amount
		}
		r = Range{Min: roundUp((l.Price + 1) / 2), Max: below(ceiling)}
	case by == Seller && open != nil && open.By == Buyer:
		r = Range{Min: open.Amount/Step*Step + Step, Max: below(l.Price)}
	default:
		return Range{}, false
	}
	if r.Min > r.Max {
		return Range{}, false
	}
	return r, true
}

// Offer opens a price: the buyer's offer, or the seller's counter to it.
func (d *Deal) Offer(by Party, amount int64, l Listing, now time.Time) error {
	if l.Status != ListingActive {
		return ErrListingUnavailable
	}
	r, ok := d.OfferRange(by, l)
	if !ok {
		return ErrMoveNotAllowed
	}
	if amount%Step != 0 {
		return ErrInvalidAmount.WithField("amount", "step")
	}
	if amount < r.Min || amount > r.Max {
		return ErrInvalidAmount.WithField("amount", "out_of_range")
	}
	kind := MoveOffer
	if by == Seller {
		kind = MoveCounter
	}
	d.open, d.listedPrice = &OpenPrice{By: by, Amount: amount, At: now}, l.Price
	d.record(kind, by, amount, now)
	return nil
}

// Accept agrees the deal at price, the price the person saw: the other
// side's open price or, for the buyer, the listed price.
func (d *Deal) Accept(by Party, price int64, l Listing, now time.Time) error {
	if l.Status != ListingActive {
		return ErrListingUnavailable
	}
	if d.status != StatusNegotiating {
		return ErrMoveNotAllowed
	}
	open := d.live(l)
	facing := open != nil && open.By != by
	voided := open == nil && d.open != nil && d.open.By != by
	switch {
	case facing && price == open.Amount, by == Buyer && price == l.Price:
	case facing || voided || by == Buyer:
		return ErrPriceChanged
	default:
		return ErrMoveNotAllowed
	}
	d.status, d.agreedPrice, d.agreedAt, d.open = StatusAgreed, price, &now, nil
	d.record(MoveAccept, by, price, now)
	return nil
}

// Decline turns down the other side's open price.
func (d *Deal) Decline(by Party, now time.Time) error {
	if d.open == nil || d.open.By == by {
		return ErrMoveNotAllowed
	}
	d.open = nil
	d.record(MoveDecline, by, 0, now)
	return nil
}

// Withdraw takes back by's own open price.
func (d *Deal) Withdraw(by Party, now time.Time) error {
	if d.open == nil || d.open.By != by {
		return ErrMoveNotAllowed
	}
	d.open = nil
	d.record(MoveWithdraw, by, 0, now)
	return nil
}

// Cancel drops the agreed price, and a requested order with it; the deal
// negotiates again. A shipped order cannot be cancelled.
func (d *Deal) Cancel(by Party, now time.Time) error {
	if d.status != StatusAgreed {
		return ErrMoveNotAllowed
	}
	if o := d.openOrder(); o != nil {
		if o.Status != OrderRequested {
			return ErrMoveNotAllowed
		}
		reason := CancelByBuyer
		if by == Seller {
			reason = CancelBySeller
		}
		d.cancelOrder(by, reason, now)
		return nil
	}
	d.status, d.agreedPrice, d.agreedAt = StatusNegotiating, 0, nil
	d.record(MoveCancel, by, 0, now)
	return nil
}

// RequestCod asks for cash on delivery to r at the agreed price.
func (d *Deal) RequestCod(by Party, r Recipient, l Listing, now time.Time) error {
	if by != Buyer || d.status != StatusAgreed || d.openOrder() != nil {
		return ErrMoveNotAllowed
	}
	if l.Status != ListingActive {
		return ErrListingUnavailable
	}
	d.order = &Order{ID: uuid.NewV7(), Status: OrderRequested, Price: d.agreedPrice, Recipient: r, RequestedAt: now}
	d.record(MoveCodRequested, by, d.agreedPrice, now)
	return nil
}

// Ship records the waybill of a requested order: the parcel is with the
// carrier.
func (d *Deal) Ship(by Party, w Waybill, l Listing, now time.Time) error {
	o := d.openOrder()
	if by != Seller || o == nil || o.Status != OrderRequested {
		return ErrMoveNotAllowed
	}
	if l.Status != ListingActive {
		return ErrListingUnavailable
	}
	o.Status, o.Waybill, o.ShippedAt = OrderShipped, &w, &now
	d.record(MoveShipped, by, w.Fee, now)
	return nil
}

// Deliver ends a shipped order delivered; the deal is sold.
func (d *Deal) Deliver(by Party, now time.Time) error {
	o := d.openOrder()
	if by != Seller || o == nil || o.Status != OrderShipped {
		return ErrMoveNotAllowed
	}
	o.Status, o.ClosedAt, d.status = OrderDelivered, &now, StatusSold
	d.record(MoveDelivered, by, 0, now)
	return nil
}

// Return ends a shipped order returned; the deal negotiates again.
func (d *Deal) Return(by Party, now time.Time) error {
	o := d.openOrder()
	if by != Seller || o == nil || o.Status != OrderShipped {
		return ErrMoveNotAllowed
	}
	o.Status, o.ClosedAt = OrderReturned, &now
	d.status, d.agreedPrice, d.agreedAt = StatusNegotiating, 0, nil
	d.record(MoveReturned, by, 0, now)
	return nil
}

// ListingChanged brings the deal in line with its listing and reports
// whether it changed. A requested order ends when the listing is hidden,
// gone or reserved for another order; a shipped one stays. Otherwise a
// hidden or gone listing takes the open price back, a gone one the
// agreement too; a new listed price, or offers turned off, takes the open
// price back. A second call with the same listing changes nothing.
func (d *Deal) ListingChanged(l Listing, now time.Time) bool {
	if o := d.openOrder(); o != nil {
		if o.Status != OrderRequested {
			return false
		}
		switch {
		case l.Status == ListingHidden || l.Status == ListingGone:
			d.cancelOrder(System, CancelListingClosed, now)
		case l.Status == ListingReserved && l.ReservedFor != o.ID:
			d.cancelOrder(System, CancelSentToAnother, now)
		default:
			return false
		}
		return true
	}
	if d.status == StatusAgreed {
		if l.Status != ListingGone {
			return false
		}
		d.status, d.agreedPrice, d.agreedAt = StatusNegotiating, 0, nil
		d.record(MoveListingClosed, System, 0, now)
		return true
	}
	if d.open == nil {
		return false
	}
	var (
		kind   MoveKind
		amount int64
	)
	switch {
	case l.Status == ListingHidden || l.Status == ListingGone:
		kind = MoveListingClosed
	case l.Price != d.listedPrice:
		kind, amount = MovePriceChanged, l.Price
	case !l.AllowOffers:
		kind = MoveOffersClosed
	default:
		return false
	}
	d.open = nil
	d.record(kind, System, amount, now)
	return true
}

// Active reports a deal still negotiating or agreed.
func (d *Deal) Active() bool { return d.status == StatusNegotiating || d.status == StatusAgreed }

// TurnOf reports whether by has to act: the deal negotiates and the open
// price l still stands behind is the other side's, or the deal is agreed
// and waits for the buyer's COD request on a listing still on sale or, on
// an order requested or shipped, for the seller.
func (d *Deal) TurnOf(by Party, l Listing) bool {
	switch d.status {
	case StatusNegotiating:
		open := d.live(l)
		return open != nil && open.By != by
	case StatusAgreed:
		if by == Seller {
			return d.openOrder() != nil
		}
		return d.openOrder() == nil && l.Status == ListingActive
	case StatusSold:
		return false
	}
	return false
}

// openOrder is the deal's order while requested or shipped, nil otherwise.
func (d *Deal) openOrder() *Order {
	if d.order != nil && d.order.Open() {
		return d.order
	}
	return nil
}

// cancelOrder ends the requested order and drops the agreement.
func (d *Deal) cancelOrder(by Party, reason CancelReason, now time.Time) {
	d.order.Status, d.order.CancelledBy, d.order.CancelReason, d.order.ClosedAt = OrderCancelled, by, reason, &now
	d.status, d.agreedPrice, d.agreedAt = StatusNegotiating, 0, nil
	d.record(MoveOrderCancelled, by, 0, now)
}

// record adds a move to the ones the next save writes.
func (d *Deal) record(kind MoveKind, by Party, amount int64, now time.Time) {
	d.pending = append(d.pending, Move{ID: uuid.NewV7(), Kind: kind, By: by, Amount: amount, At: now})
	d.updatedAt = now
}

// roundUp is the smallest multiple of Step at or above v.
func roundUp(v int64) int64 { return (v + Step - 1) / Step * Step }

// below is the largest multiple of Step under v.
func below(v int64) int64 { return (v - 1) / Step * Step }

func (d *Deal) ID() uuid.UUID        { return d.id }
func (d *Deal) ListingID() uuid.UUID { return d.listingID }
func (d *Deal) BuyerID() uuid.UUID   { return d.buyerID }
func (d *Deal) SellerID() uuid.UUID  { return d.sellerID }
func (d *Deal) Status() Status       { return d.status }
func (d *Deal) AgreedPrice() int64   { return d.agreedPrice }
func (d *Deal) AgreedAt() *time.Time { return d.agreedAt }
func (d *Deal) WriteID() uuid.UUID   { return d.writeID }
func (d *Deal) CreatedAt() time.Time { return d.createdAt }
func (d *Deal) UpdatedAt() time.Time { return d.updatedAt }

// Order returns a copy of the deal's latest order, nil before the first.
func (d *Deal) Order() *Order {
	if d.order == nil {
		return nil
	}
	return new(*d.order)
}

// Open returns a copy of the open price, nil when none is open.
func (d *Deal) Open() *OpenPrice {
	if d.open == nil {
		return nil
	}
	return new(*d.open)
}

// Pending returns the moves made since the deal was loaded. Stores only.
func (d *Deal) Pending() []Move { return append([]Move{}, d.pending...) }

// DealState is every stored field of a deal, with its latest order. Stores
// and the flows that journal a deal only.
type DealState struct {
	ID          uuid.UUID  `json:"id"`
	ListingID   uuid.UUID  `json:"listing_id"`
	BuyerID     uuid.UUID  `json:"buyer_id"`
	SellerID    uuid.UUID  `json:"seller_id"`
	Status      Status     `json:"status"`
	Open        *OpenPrice `json:"open,omitempty"`
	ListedPrice int64      `json:"listed_price"`
	AgreedPrice int64      `json:"agreed_price"`
	AgreedAt    *time.Time `json:"agreed_at,omitempty"`
	Order       *Order     `json:"order,omitempty"`
	WriteID     uuid.UUID  `json:"write_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// RestoreDeal rebuilds a stored deal. Stores only.
func RestoreDeal(st DealState) Deal {
	return Deal{
		id: st.ID, listingID: st.ListingID, buyerID: st.BuyerID, sellerID: st.SellerID, status: st.Status,
		open: st.Open, listedPrice: st.ListedPrice, agreedPrice: st.AgreedPrice, agreedAt: st.AgreedAt,
		order: st.Order, writeID: st.WriteID, createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
}

// State returns every stored field. Stores only.
func (d *Deal) State() DealState {
	return DealState{
		ID: d.id, ListingID: d.listingID, BuyerID: d.buyerID, SellerID: d.sellerID, Status: d.status,
		Open: d.Open(), ListedPrice: d.listedPrice, AgreedPrice: d.agreedPrice, AgreedAt: d.agreedAt,
		Order: d.Order(), WriteID: d.writeID, CreatedAt: d.createdAt, UpdatedAt: d.updatedAt,
	}
}

// Saved records that a store wrote d and its pending moves under
// writeID. Stores only.
func (d *Deal) Saved(writeID uuid.UUID) { d.writeID, d.pending = writeID, nil }

// OpenFor returns a copy of the open price l still stands behind, nil when
// none is open or l has voided it.
func (d *Deal) OpenFor(l Listing) *OpenPrice {
	if o := d.live(l); o != nil {
		return new(*o)
	}
	return nil
}

// live is the open price l still stands behind. A new listed price, offers
// turned off, or a listing hidden or gone voids it at once; ListingChanged
// records that later.
func (d *Deal) live(l Listing) *OpenPrice {
	if d.open == nil || l.Price != d.listedPrice || !l.AllowOffers || l.Status == ListingHidden ||
		l.Status == ListingGone {
		return nil
	}
	return d.open
}
