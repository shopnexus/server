// Package service holds module deal's use cases: a buyer opens a deal on a
// listing, the two sides move until they agree a price, the seller ships
// and the order closes, and the deals follow their listings' changes.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/carrier"
)

// Service is module deal's contract.
type Service interface {
	OrderFor(ctx context.Context, viewer, id uuid.UUID) (OrderView, uuid.UUID, error)
	// CancelOrder cancels the caller's pending order, as its buyer or its
	// seller, and drops the agreed price.
	//
	// A repeated idempotencyKey of the same caller runs nothing more; an
	// empty one runs every call.
	//
	// UC-DEAL-06 · NFR-IDEM-01
	CancelOrder(ctx context.Context, caller, order uuid.UUID, idempotencyKey string) (DealView, error)
	// Open returns the buyer's deal on a listing, or starts one in the
	// pair's conversation; created reports a new deal.
	//
	// UC-DEAL-02 · NFR-CONC-01
	Open(ctx context.Context, buyer, listingID uuid.UUID) (v DealView, created bool, err error)
	// Get reads a deal for one of its two sides.
	Get(ctx context.Context, viewer, id uuid.UUID) (DealView, error)
	// BuyerSummary returns buyer's deal on a listing at a glance; false
	// when there is none.
	BuyerSummary(ctx context.Context, buyer, listingID uuid.UUID) (DealSummary, bool, error)
	// Moves reads the moves of ids on a deal, with its people, listing,
	// latest order and the orders the moves are steps of, for
	// notifications; an ID the deal lacks is left out, and an unknown deal
	// answers false.
	Moves(ctx context.Context, dealID uuid.UUID, ids []uuid.UUID) (MoveFacts, bool, error)
	// PostMoves writes the deal message of each move of ids on a deal that
	// belongs in chat into the deal's conversation; a move the deal lacks,
	// or one posted already, writes nothing. A move chat refuses is
	// chatdomain.ErrMisplacedMove. The worker calls it on deal.moved.
	//
	// UC-DEAL-01
	PostMoves(ctx context.Context, dealID uuid.UUID, ids []uuid.UUID) error
	// ListMine lists viewer's deals on side as, newest first.
	ListMine(ctx context.Context, viewer uuid.UUID, as domain.Party, p page.Request) (page.Page[DealView], error)
	// OpenOffers lists the buyers' open offers on seller's listings that
	// still stand, newest offer first; a page may hold fewer than the
	// limit when a change of a listing voided some.
	OpenOffers(ctx context.Context, seller uuid.UUID, p page.Request) (page.Page[DealView], error)
	// OpenOfferCount counts the buyers' open offers on seller's listings,
	// a voided one until the change of its listing is recorded.
	OpenOfferCount(ctx context.Context, seller uuid.UUID) (int, error)
	// Offer opens the caller's price: the buyer's offer or the seller's
	// counter.
	//
	// UC-DEAL-02
	Offer(ctx context.Context, caller, id uuid.UUID, amount int64) (DealView, error)
	// Accept agrees the deal at price, the price the caller saw.
	//
	// UC-DEAL-02
	Accept(ctx context.Context, caller, id uuid.UUID, price int64) (DealView, error)
	// Decline turns down the other side's open price.
	//
	// UC-DEAL-02
	Decline(ctx context.Context, caller, id uuid.UUID) (DealView, error)
	// Withdraw takes back the caller's own open price.
	//
	// UC-DEAL-02
	Withdraw(ctx context.Context, caller, id uuid.UUID) (DealView, error)
	// Cancel drops the agreed price, and the pending order with it.
	//
	// UC-DEAL-02 · UC-DEAL-06
	Cancel(ctx context.Context, caller, id uuid.UUID) (DealView, error)
	// RequestCod asks for cash on delivery at price, the price the buyer
	// saw (agreed, listed or the seller's counter), to a copy of one of the
	// buyer's addresses; a deal still negotiating agrees in the same save.
	// A repeated idempotencyKey of the same caller runs nothing more; an
	// empty one runs every call.
	//
	// UC-DEAL-03 · NFR-IDEM-01
	RequestCod(ctx context.Context, caller, id uuid.UUID, price int64, addressID uuid.UUID, idempotencyKey string) (DealView, error)
	// CreateShipment ships a pending order: a manual shipment, read from its
	// carrier, or a linked one, made at GHTK. It holds the listing and
	// returns the order's deal. A repeated idempotencyKey of the same caller
	// runs nothing more.
	//
	// UC-DEAL-04 · NFR-IDEM-01
	CreateShipment(ctx context.Context, caller, order uuid.UUID, idempotencyKey string, in ShipmentInput) (DealView, error)
	// EditShipment puts the seller's corrected carrier and code, read again
	// from the carrier, on a manual shipment and returns the order's deal.
	//
	// UC-DEAL-04 A3
	EditShipment(ctx context.Context, caller, order uuid.UUID, carrierCode, trackingCode string) (DealView, error)
	// ConfirmReceipt completes a shipped order on its buyer's word, sells
	// the listing and returns the order's deal. A repeated idempotencyKey
	// of the same caller runs nothing more.
	//
	// UC-DEAL-05 A2 · NFR-IDEM-01
	ConfirmReceipt(ctx context.Context, caller, order uuid.UUID, idempotencyKey string) (DealView, error)
	// LookupShipment reads a shipment with the caller's account at its
	// carrier, for the seller's preview of a manual shipment.
	//
	// UC-DEAL-04
	LookupShipment(ctx context.Context, caller uuid.UUID, carrierCode, trackingCode string) (ShipmentPreview, error)
	// OpenShipmentsAt tells whether a shipped order of the seller's has a
	// shipment at carrier; module shipping refuses to unlink it then.
	OpenShipmentsAt(ctx context.Context, seller uuid.UUID, carrier string) (bool, error)
	// QuoteFacts reads an order of the caller's as its seller, for module
	// shipping to price: the recipient by names, the caller's address as
	// the pickup unless address is the zero ID, and the price.
	QuoteFacts(ctx context.Context, caller, order, address uuid.UUID) (to carrier.Place, from *carrier.Place, price int64, err error)
	// ShipmentUpdated sets the status of every shipped order whose
	// shipment has trackingCode, as its carrier reports it. A closed order
	// changes nothing; no order with the code is domain.ErrShipmentNotFound.
	//
	// UC-DEAL-05 · NFR-IDEM-01
	ShipmentUpdated(ctx context.Context, trackingCode, status string) error
	// ShipmentCarrier is the carrier of the orders, closed ones too, whose
	// shipment has trackingCode; domain.ErrShipmentNotFound for none.
	ShipmentCarrier(ctx context.Context, trackingCode string) (string, error)
	// Order reads one order for another module: its two sides, its
	// listing and its status; false for an unknown one.
	Order(ctx context.Context, id uuid.UUID) (OrderFacts, bool, error)
	// Between lists the deals of viewer and other on either side, newest
	// first, as viewer sees them.
	Between(ctx context.Context, viewer, other uuid.UUID) ([]DealView, error)
	// PairSummaries picks, for viewer and each of others, the deal of the
	// pair an inbox row shows: the newest by last move of those where it
	// is viewer's turn, else the newest active one, else the newest. A
	// pair without a deal is absent.
	PairSummaries(ctx context.Context, viewer uuid.UUID, others []uuid.UUID) (map[uuid.UUID]PairSummary, error)
	// ListOrders lists viewer's orders on side as, newest first.
	ListOrders(ctx context.Context, viewer uuid.UUID, as domain.Party, p page.Request) (page.Page[OrderItem], error)
	// ListingChanged brings a listing's deals in line with its current
	// state; the worker calls it for every change of the listing.
	//
	// UC-DEAL-02 A4 · UC-DEAL-06 A3
	ListingChanged(ctx context.Context, listingID uuid.UUID) error
	// ListingActivity reads, for each of the seller's listings among ids,
	// the buyers with a deal on it and the pending or shipped order its
	// card shows; a listing without a deal has no entry.
	ListingActivity(ctx context.Context, seller uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]ListingActivity, error)
	// OpenOrders reports an order of account's, as either side, that is
	// pending or shipped; erasure refuses to delete the account then.
	OpenOrders(ctx context.Context, account uuid.UUID) (bool, error)
	// BuyerOrders counts buyer's orders over the last domain.BuyerPeriod,
	// placed and failed, and those open now against domain.MaxOpenOrders.
	BuyerOrders(ctx context.Context, buyer uuid.UUID) (domain.BuyerOrders, error)
	// AwaitingShipment counts seller's pending orders, which wait for the
	// seller to create the shipment.
	AwaitingShipment(ctx context.Context, seller uuid.UUID) (int, error)
	// CountOrders counts the orders requested at or after since, the zero
	// time for all, in each status.
	CountOrders(ctx context.Context, since time.Time) (map[domain.OrderStatus]int, error)
	// AccountDeleted ends what a deleted account leaves open in its deals;
	// the worker calls it on account.deleted.
	AccountDeleted(ctx context.Context, account uuid.UUID) error
	// ScrubRecipients blanks the recipients of a deleted account's
	// purchases and its address copied as a pickup on its sales; the worker
	// calls it domain.KeepRecipientsFor after the deletion.
	ScrubRecipients(ctx context.Context, account uuid.UUID) error
}

// Deps is what New builds Deals from.
type Deps struct {
	Deals    repository.Deals
	Accounts accountsvc.Service
	Catalog  catalogsvc.Service
	// Chats opens the pair's conversation for a new deal and takes its
	// deal messages.
	Chats chatsvc.Service
	// Shipping makes, reads and cancels shipments on the seller's linked
	// accounts: module shipping.
	Shipping Shipping
	// Waits are how long the system waits before it moves an order on:
	// domain.OrderWaits outside tests. New refuses a zero wait.
	Waits domain.Waits
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
	Log     *slog.Logger
}

// flowHandler is the handler of every flow service the module calls.
const flowHandler = "Run"

// Deals implements Service in this process.
type Deals struct {
	deals    repository.Deals
	accounts accountsvc.Service
	catalog  catalogsvc.Service
	chats    chatsvc.Service
	shipping Shipping
	waits    domain.Waits
	ingress  *ingress.Client
	prefix   string
	now      func() time.Time
	log      *slog.Logger
	ship     restatex.Caller[FlowInput, FlowResult]
	confirm  restatex.Caller[FlowInput, FlowResult]
	// requestCod and cancelOrder run DealRequestCod and DealCancelOrder.
	requestCod  restatex.Caller[CodInput, FlowResult]
	cancelOrder restatex.Caller[FlowInput, FlowResult]
	shipment    restatex.Caller[ShipmentSignal, ShipmentResult]
}

var _ Service = (*Deals)(nil)

// New builds Deals. A zero wait would move every order on the moment it
// is made, so New panics on one.
func New(d Deps) *Deals {
	if d.Waits.Seller <= 0 || d.Waits.Pickup <= 0 || d.Waits.Complete <= 0 || d.Waits.Poll <= 0 {
		panic("deal: Deps.Waits needs every wait; pass domain.OrderWaits")
	}
	return &Deals{
		deals: d.Deals, accounts: d.Accounts, catalog: d.Catalog, chats: d.Chats, shipping: d.Shipping, waits: d.Waits, ingress: d.Ingress,
		prefix: d.Prefix, now: d.Now, log: d.Log,
		ship:        restatex.Caller[FlowInput, FlowResult]{Ingress: d.Ingress, Service: d.Prefix + dealShip, Handler: flowHandler},
		confirm:     restatex.Caller[FlowInput, FlowResult]{Ingress: d.Ingress, Service: d.Prefix + dealConfirm, Handler: flowHandler},
		requestCod:  restatex.Caller[CodInput, FlowResult]{Ingress: d.Ingress, Service: d.Prefix + dealRequestCod, Handler: flowHandler},
		cancelOrder: restatex.Caller[FlowInput, FlowResult]{Ingress: d.Ingress, Service: d.Prefix + dealCancelOrder, Handler: flowHandler},
		shipment: restatex.Caller[ShipmentSignal, ShipmentResult]{
			Ingress: d.Ingress, Service: d.Prefix + shipmentUpdates, Handler: flowHandler,
		},
	}
}

// Durable returns module deal's Restate services, for Mount to bind.
func (s *Deals) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{
		restate.Reflect(DealShip{Deals: s}), restate.Reflect(DealConfirm{Deals: s}),
		restate.Reflect(DealRequestCod{Deals: s}), restate.Reflect(DealCancelOrder{Deals: s}),
		restate.Reflect(OrderTimers{Deals: s}), restate.Reflect(ShipmentUpdates{Deals: s}),
	}
}

// listing reads one listing through catalog, in any status; an unknown
// one is catalogdomain.ErrNotFound.
func (s *Deals) listing(ctx context.Context, id uuid.UUID) (catalogsvc.Summary, error) {
	l, ok, err := s.catalog.Summary(ctx, id)
	if err != nil {
		return catalogsvc.Summary{}, fmt.Errorf("read listing: %w", err)
	}
	if !ok {
		return catalogsvc.Summary{}, catalogdomain.ErrNotFound
	}
	return l, nil
}

// own loads a deal and the caller's side of it. Anyone else gets
// domain.ErrNotFound, so a deal's existence does not leak.
func (s *Deals) own(ctx context.Context, caller, id uuid.UUID) (domain.Deal, domain.Party, error) {
	d, err := s.deals.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Deal{}, "", domain.ErrNotFound
	}
	if err != nil {
		return domain.Deal{}, "", fmt.Errorf("get deal: %w", err)
	}
	by, ok := d.PartyOf(caller)
	if !ok {
		return domain.Deal{}, "", domain.ErrNotFound
	}
	return d, by, nil
}

// invocationKey is the Restate idempotency key of a caller's
// Idempotency-Key, which runs apart from another caller's same key; an
// empty key gives none, so every call runs.
func invocationKey(caller uuid.UUID, idempotencyKey string) string {
	if idempotencyKey == "" {
		return ""
	}
	return caller.String() + "/" + idempotencyKey
}

// moved is the event of a deal's new moves.
func moved(id uuid.UUID, now time.Time) events.Event {
	return events.Event{ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: id, OccurredAt: now}
}

// MovedIDs reads the IDs of the moves a deal.moved carries; an ID that does
// not parse is left out.
func MovedIDs(e events.Event) []uuid.UUID {
	raw, _ := e.Payload["moves"].([]any) //nolint:revive // an event without moves names none
	ids := make([]uuid.UUID, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if id, err := uuid.Parse(s); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
