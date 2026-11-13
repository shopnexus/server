// Package service holds module shipping's use cases: a seller links and
// unlinks carrier accounts, lists the carrier's pickup points and prices a
// shipment; module deal makes, reads and cancels shipments with them; the
// carriers report through webhooks.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/provider/carrier"
	"github.com/shopnexus/server2/internal/shipping/domain"
	"github.com/shopnexus/server2/internal/shipping/repository"
)

// maxWeightGrams is the heaviest parcel, as deal takes it.
const maxWeightGrams = 20_000

// AccountView is the CarrierAccount schema.
type AccountView struct {
	Carrier  string    `json:"carrier"`
	ShopCode string    `json:"shop_code"`
	LinkedAt time.Time `json:"linked_at"`
}

// QuoteRequest is GET /v1/shipments/quote's query: an order of the
// caller's and the linked shipment they consider.
type QuoteRequest struct {
	OrderID       uuid.UUID
	Service       string
	WeightGrams   int
	PickupPointID string
	AddressID     uuid.UUID
}

// QuoteFacts is what deal tells of an order to price: where it goes, its
// price, and the pickup place when an address was asked for.
type QuoteFacts struct {
	Recipient carrier.Place
	Pickup    *carrier.Place
	Price     int64
}

// Deals is what shipping asks of module deal, which it does not import:
// the app wiring closes these over deal's service.
type Deals struct {
	// ShipmentUpdated moves the orders whose shipment has code; NotFound
	// when none has.
	ShipmentUpdated func(ctx context.Context, code, status string) error
	// OpenShipmentsAt tells whether a shipped order of the seller's is at
	// carrier c.
	OpenShipmentsAt func(ctx context.Context, seller uuid.UUID, c string) (bool, error)
	// QuoteFacts reads an order of the caller's, and the caller's address
	// when it is not the zero ID.
	QuoteFacts func(ctx context.Context, caller, order, address uuid.UUID) (QuoteFacts, error)
	// NotFound is deal's error for a code no order has.
	NotFound error
}

type Deps struct {
	Accounts repository.Accounts
	// Clients holds one client per carrier; empty is CARRIER=none.
	Clients map[carrier.Carrier]carrier.Client
	// Key seals the tokens, 32 bytes.
	Key   []byte
	Deals Deals
	Now   func() time.Time
}

// Service is module shipping's contract.
type Service interface {
	// Accounts lists the owner's linked accounts, without their tokens.
	Accounts(ctx context.Context, owner uuid.UUID) ([]AccountView, error)
	// Link checks the credentials at carrier c and keeps the token sealed,
	// replacing an account linked there before.
	Link(ctx context.Context, owner uuid.UUID, c string, cr carrier.Credentials) (AccountView, error)
	// Unlink drops the owner's account at c, refused while a shipment of
	// theirs is open there.
	Unlink(ctx context.Context, owner uuid.UUID, c string) error
	// Pickups lists the owner's pickup points at c, read from the carrier.
	Pickups(ctx context.Context, owner uuid.UUID, c string) ([]carrier.PickupPoint, error)
	// Quote is the carrier's fee for a linked shipment of an order.
	Quote(ctx context.Context, owner uuid.UUID, c string, in QuoteRequest) (int64, error)
	// Create, Lookup and Cancel are module deal's: a shipment on the
	// seller's account at c. carrier.ErrNotFound and
	// carrier.ErrNotCancellable come back as they are.
	Create(ctx context.Context, seller uuid.UUID, c carrier.Carrier, in carrier.CreateInput) (carrier.Created, error)
	Lookup(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error)
	Cancel(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) error
	// Reported takes a carrier's report of a status; a code no order has
	// changes nothing.
	Reported(ctx context.Context, c carrier.Carrier, code string, s carrier.Status) error
	// AccountDeleted drops the account's carrier accounts.
	AccountDeleted(ctx context.Context, owner uuid.UUID) error
}

type Shipping struct{ d Deps }

var _ Service = (*Shipping)(nil)

func New(d Deps) *Shipping { return &Shipping{d: d} }

// linkable tells the carriers a seller can link.
func linkable(c string) bool { return c == string(carrier.GHTK) || c == string(carrier.GHN) }

// client is carrier c's client: ErrUnavailable with no clients at all,
// before anything else, and ErrInvalidCarrier for a carrier without one.
func (s *Shipping) client(c string) (carrier.Client, error) {
	if len(s.d.Clients) == 0 {
		return nil, domain.ErrUnavailable
	}
	cl, ok := s.d.Clients[carrier.Carrier(c)]
	if !ok {
		return nil, domain.ErrInvalidCarrier
	}
	return cl, nil
}

// account is the client of c and the owner's account there, its token
// opened. A carrier without a client, one no seller can link, is one the
// owner has not linked: ErrNotLinked.
func (s *Shipping) account(ctx context.Context, owner uuid.UUID, c string) (carrier.Client, carrier.Account, error) {
	cl, err := s.client(c)
	if errors.Is(err, domain.ErrInvalidCarrier) {
		return nil, carrier.Account{}, domain.ErrNotLinked
	}
	if err != nil {
		return nil, carrier.Account{}, err
	}
	row, err := s.d.Accounts.Get(ctx, owner, c)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, carrier.Account{}, domain.ErrNotLinked
	}
	if err != nil {
		return nil, carrier.Account{}, fmt.Errorf("read the %s account of %s: %w", c, owner, err)
	}
	token, err := domain.Open(s.d.Key, row.Token, row.Nonce)
	if err != nil {
		return nil, carrier.Account{}, domain.ErrRelinkNeeded.WithCause(fmt.Errorf("open the %s token of %s: %w", c, owner, err))
	}
	return cl, carrier.Account{Token: token, ShopCode: row.ShopCode}, nil
}

// carrierError turns a client's error into the API's; carrier.ErrNotFound
// and carrier.ErrNotCancellable stay for deal to read.
func carrierError(err error) error {
	switch {
	case errors.Is(err, carrier.ErrUnavailable):
		return domain.ErrUnavailable
	case errors.Is(err, carrier.ErrLoginFailed):
		return domain.ErrLoginFailed
	case errors.Is(err, carrier.ErrAddressUnknown):
		return domain.ErrAddressUnknown
	case errors.Is(err, carrier.ErrRefused):
		reason := strings.TrimSpace(strings.TrimPrefix(err.Error(), carrier.ErrRefused.Error()+":"))
		return domain.ErrCarrierRefused.WithDetail("reason", reason).WithCause(err)
	}
	return err
}

func (s *Shipping) Accounts(ctx context.Context, owner uuid.UUID) ([]AccountView, error) {
	rows, err := s.d.Accounts.List(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("list the carrier accounts: %w", err)
	}
	views := make([]AccountView, 0, len(rows))
	for _, r := range rows {
		views = append(views, AccountView{Carrier: r.Carrier, ShopCode: r.ShopCode, LinkedAt: r.LinkedAt})
	}
	return views, nil
}

func (s *Shipping) Link(ctx context.Context, owner uuid.UUID, c string, cr carrier.Credentials) (AccountView, error) {
	cl, err := s.client(c)
	if err != nil {
		return AccountView{}, err
	}
	l, err := cl.Link(ctx, cr)
	if err != nil {
		return AccountView{}, carrierError(err)
	}
	sealed, nonce, err := domain.Seal(s.d.Key, l.Token)
	if err != nil {
		return AccountView{}, err
	}
	row := repository.Row{AccountID: owner, Carrier: c, ShopCode: l.ShopCode, Token: sealed, Nonce: nonce, LinkedAt: s.d.Now().UTC()}
	if err = s.d.Accounts.Put(ctx, row); err != nil {
		return AccountView{}, fmt.Errorf("store the %s account: %w", c, err)
	}
	return AccountView{Carrier: c, ShopCode: row.ShopCode, LinkedAt: row.LinkedAt}, nil
}

func (s *Shipping) Unlink(ctx context.Context, owner uuid.UUID, c string) error {
	if !linkable(c) {
		return domain.ErrInvalidCarrier
	}
	open, err := s.d.Deals.OpenShipmentsAt(ctx, owner, c)
	if err != nil {
		return fmt.Errorf("read the open shipments at %s: %w", c, err)
	}
	if open {
		return domain.ErrShipmentsOpen
	}
	deleted, err := s.d.Accounts.Delete(ctx, owner, c)
	if err != nil {
		return fmt.Errorf("unlink %s: %w", c, err)
	}
	if !deleted {
		return domain.ErrAccountNotFound
	}
	return nil
}

func (s *Shipping) Pickups(ctx context.Context, owner uuid.UUID, c string) ([]carrier.PickupPoint, error) {
	if _, err := s.client(c); err != nil {
		return nil, err
	}
	cl, a, err := s.account(ctx, owner, c)
	if err != nil {
		return nil, err
	}
	pts, err := cl.Pickups(ctx, a)
	return pts, carrierError(err)
}

// Quote checks the shipment as deal checks one to make, reads the order,
// which must be the owner's, then asks the carrier on the owner's account.
func (s *Shipping) Quote(ctx context.Context, owner uuid.UUID, c string, in QuoteRequest) (int64, error) {
	cl, err := s.client(c)
	if err != nil {
		return 0, err
	}
	invalid := domain.ErrInvalidShipment
	if in.Service != "standard" && in.Service != "express" {
		invalid = invalid.WithField("service", "oneOf")
	}
	if in.WeightGrams < 1 || in.WeightGrams > maxWeightGrams {
		invalid = invalid.WithField("weight_grams", "range")
	}
	if in.PickupPointID != "" && in.AddressID != uuid.Nil() {
		invalid = invalid.WithField("pickup_from", "oneOf")
	}
	if len(invalid.Fields) > 0 {
		return 0, invalid
	}
	facts, err := s.d.Deals.QuoteFacts(ctx, owner, in.OrderID, in.AddressID)
	if err != nil {
		return 0, err
	}
	_, a, err := s.account(ctx, owner, c)
	if err != nil {
		return 0, err
	}
	fee, err := cl.Quote(ctx, a, carrier.QuoteInput{
		Recipient: facts.Recipient, Pickup: carrier.Pickup{PointID: in.PickupPointID, Place: facts.Pickup},
		WeightGrams: in.WeightGrams, Service: in.Service, Value: facts.Price,
	})
	return fee, carrierError(err)
}

func (s *Shipping) Create(ctx context.Context, seller uuid.UUID, c carrier.Carrier, in carrier.CreateInput) (carrier.Created, error) {
	cl, a, err := s.account(ctx, seller, string(c))
	if err != nil {
		return carrier.Created{}, err
	}
	created, err := cl.Create(ctx, a, in)
	return created, carrierError(err)
}

func (s *Shipping) Lookup(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error) {
	cl, a, err := s.account(ctx, seller, string(c))
	if err != nil {
		return carrier.Parcel{}, err
	}
	p, err := cl.Lookup(ctx, a, code)
	return p, carrierError(err)
}

func (s *Shipping) Cancel(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) error {
	cl, a, err := s.account(ctx, seller, string(c))
	if err != nil {
		return err
	}
	return carrierError(cl.Cancel(ctx, a, code))
}

func (s *Shipping) Reported(ctx context.Context, c carrier.Carrier, code string, st carrier.Status) error {
	err := s.d.Deals.ShipmentUpdated(ctx, code, string(st))
	if errors.Is(err, s.d.Deals.NotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s reported %s %s: %w", c, code, st, err)
	}
	return nil
}

func (s *Shipping) AccountDeleted(ctx context.Context, owner uuid.UUID) error {
	if err := s.d.Accounts.DeleteAll(ctx, owner); err != nil {
		return fmt.Errorf("drop the carrier accounts: %w", err)
	}
	return nil
}
