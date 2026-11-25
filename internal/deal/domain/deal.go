// Package domain holds module deal's entity: a buyer and a seller agreeing
// a price on one listing, move by move.
package domain

import (
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// Step is the unit of every offer and counter, in đồng.
const Step = 1_000

type Status string

const (
	StatusNegotiating Status = "negotiating"
	StatusAgreed      Status = "agreed"
	// StatusSold is a deal whose order was completed; it takes no move.
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

// ListRole is the role query of the caller's deal and order lists: buying
// lists them as the buyer, selling as the seller.
type ListRole string

const (
	RoleBuying  ListRole = "buying"
	RoleSelling ListRole = "selling"
)

// Party is the caller's side in the deals a list of role r holds.
func (r ListRole) Party() Party {
	if r == RoleSelling {
		return Seller
	}
	return Buyer
}

type MoveKind string

const (
	MoveOffer           MoveKind = "offer"
	MoveCounter         MoveKind = "counter"
	MoveAccept          MoveKind = "accept"
	MoveDecline         MoveKind = "decline"
	MoveWithdraw        MoveKind = "withdraw"
	MoveCancel          MoveKind = "cancel"
	MovePriceChanged    MoveKind = "price_changed"
	MoveOffersClosed    MoveKind = "offers_closed"
	MoveListingClosed   MoveKind = "listing_closed"
	MoveCodRequested    MoveKind = "cod_requested"
	MoveShipped         MoveKind = "shipped"
	MoveShipmentUpdated MoveKind = "shipment_updated"
	MoveCompleted       MoveKind = "completed"
	MoveFailed          MoveKind = "failed"
	MoveOrderCancelled  MoveKind = "order_cancelled"
)

// Move is one step of a deal's thread. Amount is 0 on a move without one;
// on price_changed it is the new listed price, on cod_requested the price,
// on shipped the carrier's fee when it gave one. OrderID is the order a
// step of an order belongs to; nil on any other move, and on an order step
// that the release before wrote or that migration 0052 could not match to
// its order. A pending move has no ID: the store derives it from the
// save's write ID, so a retried save writes the same move.
type Move struct {
	ID      uuid.UUID
	Kind    MoveKind
	By      Party
	Amount  int64
	At      time.Time
	OrderID *uuid.UUID
}

// OfOrder reports whether a move of kind k is a step of an order: its
// request, its shipment and the shipment's updates, and its end.
func (k MoveKind) OfOrder() bool {
	switch k {
	case MoveCodRequested, MoveShipped, MoveShipmentUpdated, MoveCompleted, MoveFailed, MoveOrderCancelled:
		return true
	case MoveOffer, MoveCounter, MoveAccept, MoveDecline, MoveWithdraw, MoveCancel, MovePriceChanged,
		MoveOffersClosed, MoveListingClosed:
		return false
	default:
		return false
	}
}

// InChat reports whether the move writes a deal message into the pair's
// conversation: every move a person makes and every order step, whoever
// made it. A change of the listing (price_changed, offers_closed,
// listing_closed) writes none.
func (m Move) InChat() bool {
	switch m.Kind {
	case MovePriceChanged, MoveOffersClosed, MoveListingClosed:
		return false
	case MoveOffer, MoveCounter, MoveAccept, MoveDecline, MoveWithdraw, MoveCancel, MoveCodRequested, MoveShipped,
		MoveShipmentUpdated, MoveCompleted, MoveFailed, MoveOrderCancelled:
		return true
	}
	return false
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

// Closed reports a listing hidden or gone.
func (l Listing) Closed() bool { return l.Status == ListingHidden || l.Status == ListingGone }

// Range is the amounts an offer may take, both ends included.
type Range struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

// Deal is a buyer's negotiation with a seller over one listing. It holds
// at most one open price; agreeing reserves nothing. It lives in the
// conversation of its buyer and seller.
type Deal struct {
	id             uuid.UUID
	conversationID uuid.UUID
	listingID      uuid.UUID
	buyerID        uuid.UUID
	sellerID       uuid.UUID
	status         Status
	open           *OpenPrice
	listedPrice    int64
	agreedPrice    int64
	agreedAt       *time.Time
	order          *Order
	writeID        uuid.UUID
	createdAt      time.Time
	updatedAt      time.Time
	pending        []Move
}

// Open starts the buyer's deal on a listing in the pair's conversation,
// negotiating with nothing open. The first write ID is the id.
func Open(id, conversation, listingID, buyer, seller uuid.UUID, now time.Time) Deal {
	return Deal{
		id: id, conversationID: conversation, listingID: listingID, buyerID: buyer, sellerID: seller,
		status: StatusNegotiating, writeID: id, createdAt: now, updatedAt: now,
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
		return ErrInvalidAmount.WithField("amount", apperr.OffStep, "step")
	}
	if amount < r.Min || amount > r.Max {
		return ErrInvalidAmount.WithField("amount", apperr.OutOfRange, "out_of_range")
	}
	kind := MoveOffer
	if by == Seller {
		kind = MoveCounter
	}
	d.open, d.listedPrice = &OpenPrice{By: by, Amount: amount, At: now}, l.Price
	d.record(kind, by, amount, now)
	return nil
}

// Accept agrees the deal at price, the other side's open price the person
// saw. The buyer takes the listed price by ordering (RequestCod).
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
	case facing && price == open.Amount:
	case facing || voided:
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

func (d *Deal) CancelPendingOrder(by Party, order uuid.UUID, now time.Time) error {
	if d.order == nil || d.order.ID != order || d.order.Status != OrderPending {
		return ErrMoveNotAllowed
	}
	return d.Cancel(by, now)
}

// Cancel drops the agreed price, and a pending order with it; the deal
// negotiates again. A shipped order cannot be cancelled.
func (d *Deal) Cancel(by Party, now time.Time) error {
	if d.status != StatusAgreed {
		return ErrMoveNotAllowed
	}
	if o := d.openOrder(); o != nil {
		if o.Status != OrderPending {
			return ErrMoveNotAllowed
		}
		reason := CancelByBuyer
		if by == Seller {
			reason = CancelBySeller
		}
		d.cancelOrder(by, reason, now)
		return nil
	}
	d.reopen()
	d.record(MoveCancel, by, 0, now)
	return nil
}

// RequestCod asks for cash on delivery to r at price, the price the buyer
// saw: the agreed price or, on a deal still negotiating, the listed price
// or the seller's open counter, which the order agrees in the same move.
// The order takes the ID order, which the caller picks.
func (d *Deal) RequestCod(by Party, order uuid.UUID, price int64, r Recipient, l Listing, now time.Time) error {
	if by != Buyer || d.status == StatusSold || d.openOrder() != nil {
		return ErrMoveNotAllowed
	}
	if l.Status != ListingActive {
		return ErrListingUnavailable
	}
	if d.status == StatusNegotiating {
		open := d.live(l)
		if price != l.Price && (open == nil || open.By != Seller || price != open.Amount) {
			return ErrPriceChanged
		}
		d.status, d.agreedPrice, d.agreedAt, d.open = StatusAgreed, price, &now, nil
	} else if price != d.agreedPrice {
		return ErrPriceChanged
	}
	d.order = &Order{ID: order, Status: OrderPending, Price: price, Recipient: r, RequestedAt: now}
	d.record(MoveCodRequested, by, price, now)
	return nil
}

// Shippable reports whether by may ship order on l now: the seller, on the
// deal's pending order, with the listing on sale. DealShip asks before it
// holds the listing, and Ship asks again.
func (d *Deal) Shippable(by Party, order uuid.UUID, l Listing) error {
	o := d.openOrder()
	if by != Seller || o == nil || o.ID != order || o.Status != OrderPending {
		return ErrMoveNotAllowed
	}
	if l.Status != ListingActive {
		return ErrListingUnavailable
	}
	return nil
}

// Ship records s, the shipment the carrier just made for the pending
// order: the order is shipped and its parcel waits for pickup.
func (d *Deal) Ship(by Party, order uuid.UUID, s Shipment, l Listing, now time.Time) error {
	if err := d.Shippable(by, order, l); err != nil {
		return err
	}
	s.Status, s.PickedUpAt, s.DeliveredAt = ShipmentPendingPickup, nil, nil
	o := d.order
	o.Status, o.Shipment, o.ShippedAt = OrderShipped, &s, &now
	var fee int64
	if s.Fee != nil {
		fee = *s.Fee
	}
	d.record(MoveShipped, by, fee, now)
	return nil
}

// ConfirmReceipt ends the shipped order completed on the buyer's word, at
// any time while it is shipped, whatever the carrier reports.
func (d *Deal) ConfirmReceipt(by Party, order uuid.UUID, now time.Time) error {
	o := d.openOrder()
	if by != Buyer || o == nil || o.ID != order || o.Status != OrderShipped {
		return ErrMoveNotAllowed
	}
	d.complete(by, now)
	return nil
}

// ShipmentUpdated is ShipmentReported of a step that happened now.
func (d *Deal) ShipmentUpdated(order uuid.UUID, status ShipmentStatus, now time.Time) (bool, error) {
	return d.ShipmentReported(order, status, now, now)
}

// ShipmentReported sets the shipped order's shipment status as its carrier
// reports it, of a step that happened at, and reports whether anything
// changed. A report no newer than the stored one changes nothing; the same
// status at a newer time only moves StatusAt. The first status past
// pending_pickup but failed sets PickedUpAt, and pending_pickup clears it;
// delivered sets DeliveredAt and leaving it clears it. failed, or lost
// after the pickup, fails the order at once: the deal negotiates again
// without its agreed price. lost before the pickup moves nothing, so the
// pickup timeout cancels the order. An order that is not the deal's
// shipped one, or one without a shipment, is ErrMoveNotAllowed: a late
// report changes nothing.
func (d *Deal) ShipmentReported(order uuid.UUID, status ShipmentStatus, at, now time.Time) (bool, error) {
	o := d.openOrder()
	if o == nil || o.ID != order || o.Status != OrderShipped || o.Shipment == nil {
		return false, ErrMoveNotAllowed
	}
	if st := o.Shipment.StatusAt; st != nil && !at.After(*st) {
		return false, nil
	}
	if status == ShipmentLost {
		if o.Shipment.PickedUpAt == nil {
			return false, nil
		}
		status = ShipmentFailed
	}
	s := *o.Shipment
	s.StatusAt = &at
	o.Shipment = &s
	if s.Status == status {
		return true, nil // a newer step of the same status: no move
	}
	s.Status, s.DeliveredAt = status, nil
	if status == ShipmentPendingPickup {
		s.PickedUpAt = nil
	} else if s.PickedUpAt == nil && status != ShipmentFailed {
		s.PickedUpAt = &now
	}
	if status == ShipmentDelivered {
		s.DeliveredAt = &now
	}
	if status == ShipmentFailed {
		d.failOrder(now)
		return true, nil
	}
	d.record(MoveShipmentUpdated, System, 0, now)
	return true, nil
}

// FeeReported sets the shipped order's shipment fee to fee, the carrier's
// newest, from a report of a step that happened at, and reports whether
// it changed. A report older than the stored step changes nothing, as in
// ShipmentReported; a closed order keeps its fee. It writes no move.
func (d *Deal) FeeReported(order uuid.UUID, fee int64, at time.Time) bool {
	o := d.openOrder()
	if o == nil || o.ID != order || o.Status != OrderShipped || o.Shipment == nil {
		return false
	}
	if st := o.Shipment.StatusAt; st != nil && at.Before(*st) {
		return false
	}
	if f := o.Shipment.Fee; f != nil && *f == fee {
		return false
	}
	s := *o.Shipment
	s.Fee = &fee
	o.Shipment = &s
	return true
}

// failOrder fails the shipped order: the deal negotiates again.
func (d *Deal) failOrder(now time.Time) {
	d.order.Status, d.order.ClosedAt = OrderFailed, &now
	d.reopen()
	d.record(MoveFailed, System, 0, now)
}

// TransitTimeout fails order when it is shipped and its carrier no longer
// knows the parcel Waits.Transit after the pickup; a delivered or closed
// order is ErrMoveNotAllowed.
func (d *Deal) TransitTimeout(order uuid.UUID, now time.Time) error {
	o := d.openOrder()
	if o == nil || o.ID != order || o.Status != OrderShipped || o.Shipment == nil ||
		o.Shipment.Status == ShipmentDelivered {
		return ErrMoveNotAllowed
	}
	s := *o.Shipment
	s.Status, s.DeliveredAt = ShipmentFailed, nil
	o.Shipment = &s
	d.failOrder(now)
	return nil
}

// SellerTimeout cancels order when it is still the deal's pending order;
// anything else is ErrMoveNotAllowed and the timer leaves the deal alone.
func (d *Deal) SellerTimeout(order uuid.UUID, now time.Time) error {
	o := d.openOrder()
	if o == nil || o.ID != order || o.Status != OrderPending {
		return ErrMoveNotAllowed
	}
	d.cancelOrder(System, CancelSellerTimeout, now)
	return nil
}

// PickupTimeout cancels order when it is shipped and its parcel still
// waits for pickup; anything else is ErrMoveNotAllowed.
func (d *Deal) PickupTimeout(order uuid.UUID, now time.Time) error {
	o := d.openOrder()
	if o == nil || o.ID != order || o.Status != OrderShipped || o.Shipment == nil ||
		o.Shipment.Status != ShipmentPendingPickup {
		return ErrMoveNotAllowed
	}
	d.cancelOrder(System, CancelPickupTimeout, now)
	return nil
}

// AutoComplete ends order completed when it is shipped and its carrier
// reports it delivered; anything else is ErrMoveNotAllowed.
func (d *Deal) AutoComplete(order uuid.UUID, now time.Time) error {
	o := d.openOrder()
	if o == nil || o.ID != order || o.Status != OrderShipped || o.Shipment == nil ||
		o.Shipment.Status != ShipmentDelivered {
		return ErrMoveNotAllowed
	}
	d.complete(System, now)
	return nil
}

// ListingChanged brings the deal in line with its listing and reports
// whether it changed. A pending order ends when the listing is hidden,
// gone or reserved for another order; a shipped one stays. Otherwise a
// hidden or gone listing takes the open price back, a gone one the
// agreement too; a new listed price, or offers turned off, takes the open
// price back. A second call with the same listing changes nothing.
func (d *Deal) ListingChanged(l Listing, now time.Time) bool {
	if o := d.openOrder(); o != nil {
		if o.Status != OrderPending {
			return false
		}
		switch {
		case l.Closed():
			d.cancelOrder(System, CancelListingClosed, now)
		case l.Status == ListingReserved && l.ReservedFor != o.ID:
			d.cancelOrder(System, CancelShippedToAnother, now)
		default:
			return false
		}
		return true
	}
	if d.status == StatusAgreed {
		if l.Status != ListingGone {
			return false
		}
		d.reopen()
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
	case l.Closed():
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
// a pending order, for the seller's shipment. Nobody's turn comes while an
// order is shipped: the carrier moves it.
func (d *Deal) TurnOf(by Party, l Listing) bool {
	switch d.status {
	case StatusNegotiating:
		open := d.live(l)
		return open != nil && open.By != by
	case StatusAgreed:
		o := d.openOrder()
		if by == Seller {
			return o != nil && o.Status == OrderPending
		}
		return o == nil && l.Status == ListingActive
	case StatusSold:
		return false
	}
	return false
}

// openOrder is the deal's order while pending or shipped, nil otherwise.
func (d *Deal) openOrder() *Order {
	if d.order != nil && d.order.Open() {
		return d.order
	}
	return nil
}

// cancelOrder ends the open order and drops the agreement.
func (d *Deal) cancelOrder(by Party, reason CancelReason, now time.Time) {
	d.order.Status, d.order.CancelledBy, d.order.CancelReason, d.order.ClosedAt = OrderCancelled, by, reason, &now
	d.reopen()
	d.record(MoveOrderCancelled, by, 0, now)
}

// complete ends the shipped order completed; the deal is sold.
func (d *Deal) complete(by Party, now time.Time) {
	d.order.Status, d.order.ClosedAt, d.order.CompletedBy, d.status = OrderCompleted, &now, by, StatusSold
	d.record(MoveCompleted, by, 0, now)
}

// reopen drops the agreement; the deal negotiates again.
func (d *Deal) reopen() { d.status, d.agreedPrice, d.agreedAt = StatusNegotiating, 0, nil }

// record adds a move to the ones the next save writes. A step of an order
// names the deal's latest order, the one it moved.
func (d *Deal) record(kind MoveKind, by Party, amount int64, now time.Time) {
	m := Move{Kind: kind, By: by, Amount: amount, At: now}
	if kind.OfOrder() {
		m.OrderID = new(d.order.ID)
	}
	d.pending = append(d.pending, m)
	d.updatedAt = now
}

// roundUp is the smallest multiple of Step at or above v.
func roundUp(v int64) int64 { return (v + Step - 1) / Step * Step }

// below is the largest multiple of Step under v.
func below(v int64) int64 { return (v - 1) / Step * Step }

func (d *Deal) ID() uuid.UUID             { return d.id }
func (d *Deal) ConversationID() uuid.UUID { return d.conversationID }
func (d *Deal) ListingID() uuid.UUID      { return d.listingID }
func (d *Deal) BuyerID() uuid.UUID        { return d.buyerID }
func (d *Deal) SellerID() uuid.UUID       { return d.sellerID }
func (d *Deal) Status() Status            { return d.status }
func (d *Deal) AgreedPrice() int64        { return d.agreedPrice }
func (d *Deal) AgreedAt() *time.Time      { return d.agreedAt }

// Order returns a copy of the deal's latest order, nil before the first.
func (d *Deal) Order() *Order {
	if d.order == nil {
		return nil
	}
	return new(*d.order)
}

// OpenPrice returns a copy of the open price, nil when none is open.
func (d *Deal) OpenPrice() *OpenPrice {
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
	ID             uuid.UUID  `json:"id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	ListingID      uuid.UUID  `json:"listing_id"`
	BuyerID        uuid.UUID  `json:"buyer_id"`
	SellerID       uuid.UUID  `json:"seller_id"`
	Status         Status     `json:"status"`
	Open           *OpenPrice `json:"open,omitempty"`
	ListedPrice    int64      `json:"listed_price"`
	AgreedPrice    int64      `json:"agreed_price"`
	AgreedAt       *time.Time `json:"agreed_at,omitempty"`
	Order          *Order     `json:"order,omitempty"`
	WriteID        uuid.UUID  `json:"write_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// RestoreDeal rebuilds a stored deal. Stores only.
func RestoreDeal(st DealState) Deal {
	return Deal{
		id: st.ID, conversationID: st.ConversationID, listingID: st.ListingID, buyerID: st.BuyerID, sellerID: st.SellerID, status: st.Status,
		open: st.Open, listedPrice: st.ListedPrice, agreedPrice: st.AgreedPrice, agreedAt: st.AgreedAt,
		order: st.Order, writeID: st.WriteID, createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
}

// State returns every stored field. Stores only.
func (d *Deal) State() DealState {
	return DealState{
		ID: d.id, ConversationID: d.conversationID, ListingID: d.listingID, BuyerID: d.buyerID, SellerID: d.sellerID, Status: d.status,
		Open: d.OpenPrice(), ListedPrice: d.listedPrice, AgreedPrice: d.agreedPrice, AgreedAt: d.agreedAt,
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
	if d.open == nil || l.Price != d.listedPrice || !l.AllowOffers || l.Closed() {
		return nil
	}
	return d.open
}
