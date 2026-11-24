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

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/domain"
	"github.com/shopnexus/server/internal/shipping/repository"
)

// maxWeightGrams is the heaviest parcel, as deal takes it.
const maxWeightGrams = 20_000

// AccountView is the CarrierAccount schema.
type AccountView struct {
	Carrier  string    `json:"carrier"`
	ShopCode string    `json:"shop_code"`
	LinkedAt time.Time `json:"linked_at"`
}

// Tracking is the Tracking schema: the step a parcel is at, empty before
// its carrier reported one, and its events, newest first.
type Tracking struct {
	Step   carrier.Step    `json:"step,omitempty"`
	Events []carrier.Event `json:"events"`
}

// QuoteRequest is GET /v1/shipments/quote's query: an order of the
// caller's and the linked shipment they consider.
type QuoteRequest struct {
	OrderID       uuid.UUID
	Service       string
	WeightGrams   int
	PickupPointID string
	AddressID     uuid.UUID
	SavedPickup   bool
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
	// ShipmentCarrier is the carrier of the orders whose shipment has
	// code, closed ones too; NotFound when none has.
	ShipmentCarrier func(ctx context.Context, code string) (string, error)
	// NotFound is deal's error for a code no order has.
	NotFound error
}

type Deps struct {
	Settings  repository.Settings
	Customers accountsvc.Service
	Accounts  repository.Accounts
	Events    repository.Events
	// Clients holds one client per carrier; empty is CARRIER=none.
	Clients map[carrier.Carrier]carrier.Client
	// Key seals the tokens, 32 bytes.
	Key   []byte
	Deals Deals
	Now   func() time.Time
}

// Service is module shipping's contract.
type Service interface {
	Settings(ctx context.Context, owner uuid.UUID) (domain.Settings, error)
	PutSettings(ctx context.Context, owner uuid.UUID, st domain.Settings) (domain.Settings, error)
	SavedPickup(ctx context.Context, owner uuid.UUID) (Pickup, error)
	// Accounts lists the owner's linked accounts, without their tokens.
	Accounts(ctx context.Context, owner uuid.UUID) ([]AccountView, error)
	// SendOTP has carrier c text phone the code Link takes; only GHN does.
	SendOTP(ctx context.Context, c, phone string) error
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
	// Track is Lookup for an order's own shipment: it also keeps the
	// events the carrier tells, and its Step is the timeline's newest, the
	// one a report moves the order to.
	Track(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error)
	// Reported reads carrier c's webhook, keeps the events it tells of a
	// parcel and moves its orders to the status of its newest step. A code
	// no order has changes nothing; a report that does not read is
	// domain.ErrReportUnread.
	Reported(ctx context.Context, c carrier.Carrier, contentType string, body []byte) error
	// DevReported is a report as the dev endpoint makes it, for the fake
	// carrier: the carrier is the order's, and a code no order has is
	// deal's shipment_not_found.
	DevReported(ctx context.Context, code string, ev carrier.Event) error
	// Tracking is a parcel's timeline as the carriers reported it: its
	// steps, and for the seller also the weight, fee and COD paid out.
	Tracking(ctx context.Context, c, code string, seller bool) (Tracking, error)
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
	if row.Token == nil {
		return cl, carrier.Account{ShopCode: row.ShopCode}, nil
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
	case errors.Is(err, carrier.ErrPhoneUnknown):
		return domain.ErrPhoneUnknown
	case errors.Is(err, carrier.ErrOTPInvalid):
		return domain.ErrOTPInvalid
	case errors.Is(err, carrier.ErrRefused):
		reason := strings.TrimSpace(strings.TrimPrefix(err.Error(), carrier.ErrRefused.Error()+":"))
		return domain.ErrCarrierRefused.WithReason(reason).WithCause(err)
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

// mobile is raw as +84 and nine digits, or carrier_phone_unknown.
func mobile(raw string) (string, error) {
	p, err := contact.ParsePhone(raw)
	if err != nil {
		return "", domain.ErrPhoneUnknown.WithField("phone", apperr.Invalid, "format")
	}
	return p.Value(), nil
}

func (s *Shipping) SendOTP(ctx context.Context, c, phone string) error {
	cl, err := s.client(c)
	if err != nil {
		return err
	}
	t, ok := cl.(carrier.Texter)
	if !ok || c != string(carrier.GHN) {
		return domain.ErrInvalidCarrier
	}
	if phone, err = mobile(phone); err != nil {
		return err
	}
	return carrierError(t.SendOTP(ctx, phone))
}

func (s *Shipping) Link(ctx context.Context, owner uuid.UUID, c string, cr carrier.Credentials) (AccountView, error) {
	cl, err := s.client(c)
	if err != nil {
		return AccountView{}, err
	}
	if cr.OTP != "" {
		if cr.Phone, err = mobile(cr.Phone); err != nil {
			return AccountView{}, err
		}
	}
	l, err := cl.Link(ctx, cr)
	if err != nil {
		return AccountView{}, carrierError(err)
	}
	row := repository.Row{AccountID: owner, Carrier: c, ShopCode: l.ShopCode, LinkedAt: s.d.Now().UTC()}
	// A shop ShopNexus joined keeps no token of its own.
	if l.Token != "" {
		if row.Token, row.Nonce, err = domain.Seal(s.d.Key, l.Token); err != nil {
			return AccountView{}, err
		}
	}
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
		invalid = invalid.WithField("service", apperr.NotAllowed, "oneOf")
	}
	if in.WeightGrams < 1 || in.WeightGrams > maxWeightGrams {
		invalid = invalid.WithField("weight_grams", apperr.OutOfRange, "range")
	}
	if (in.PickupPointID != "" && in.AddressID != uuid.Nil()) || (in.SavedPickup && (in.PickupPointID != "" || in.AddressID != uuid.Nil())) {
		invalid = invalid.WithField("pickup_from", apperr.OneOf, "oneOf")
	}
	if len(invalid.Fields) > 0 {
		return 0, invalid
	}
	facts, err := s.d.Deals.QuoteFacts(ctx, owner, in.OrderID, in.AddressID)
	if err != nil {
		return 0, err
	}
	if in.SavedPickup {
		p, pickupErr := s.SavedPickup(ctx, owner)
		if pickupErr != nil {
			return 0, pickupErr
		}
		facts.Pickup = &carrier.Place{Name: p.Name, Phone: p.Phone, Street: p.Street, Commune: p.Commune, Province: p.Province}
	}
	_, a, err := s.account(ctx, owner, c)
	if err != nil {
		return 0, err
	}
	from, err := s.joinedPickup(ctx, owner, a, carrier.Pickup{PointID: in.PickupPointID, Place: facts.Pickup})
	if err != nil {
		return 0, err
	}
	fee, err := cl.Quote(ctx, a, carrier.QuoteInput{
		Recipient: facts.Recipient, Pickup: from,
		WeightGrams: in.WeightGrams, Service: in.Service, Value: facts.Price,
	})
	return fee, carrierError(err)
}

// joinedPickup is the saved pickup for a shop ShopNexus joined and no
// place asked: that shop's own address at GHN is only a fallback.
func (s *Shipping) joinedPickup(ctx context.Context, owner uuid.UUID, a carrier.Account, p carrier.Pickup) (carrier.Pickup, error) {
	if a.Token != "" || p.Place != nil {
		return p, nil
	}
	sp, err := s.SavedPickup(ctx, owner)
	if err != nil {
		return carrier.Pickup{}, err
	}
	return carrier.Pickup{Place: &carrier.Place{Name: sp.Name, Phone: sp.Phone, Street: sp.Street, Commune: sp.Commune, Province: sp.Province}}, nil
}

func (s *Shipping) Create(ctx context.Context, seller uuid.UUID, c carrier.Carrier, in carrier.CreateInput) (carrier.Created, error) {
	cl, a, err := s.account(ctx, seller, string(c))
	if err != nil {
		return carrier.Created{}, err
	}
	if in.Pickup, err = s.joinedPickup(ctx, seller, a, in.Pickup); err != nil {
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

func (s *Shipping) Track(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error) {
	p, err := s.Lookup(ctx, seller, c, code)
	if err != nil {
		return carrier.Parcel{}, err
	}
	if _, err = s.keep(ctx, string(c), code, p.Events); err != nil {
		return carrier.Parcel{}, err
	}
	t, err := s.Tracking(ctx, string(c), code, false)
	if err != nil {
		return carrier.Parcel{}, err
	}
	if t.Step != "" {
		p.Step = t.Step
	}
	return p, nil
}

func (s *Shipping) Reported(ctx context.Context, c carrier.Carrier, contentType string, body []byte) error {
	cl, err := s.client(string(c))
	if err != nil {
		return err
	}
	r, ok := cl.(carrier.Reporter)
	if !ok {
		return fmt.Errorf("%w: %s posts none", domain.ErrReportUnread, c)
	}
	code, evs, err := r.Report(contentType, body, s.d.Now())
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrReportUnread, err)
	}
	if len(evs) == 0 {
		return nil
	}
	owner, err := s.d.Deals.ShipmentCarrier(ctx, code)
	if errors.Is(err, s.d.Deals.NotFound) || (err == nil && owner != string(c)) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find %s %s: %w", c, code, err)
	}
	return s.report(ctx, string(c), code, evs)
}

func (s *Shipping) DevReported(ctx context.Context, code string, ev carrier.Event) error {
	c, err := s.d.Deals.ShipmentCarrier(ctx, code)
	if err != nil {
		return fmt.Errorf("find %s: %w", code, err)
	}
	return s.report(ctx, c, code, []carrier.Event{ev})
}

// report keeps evs and, when a new step among them is the parcel's
// newest, moves the orders to its status. A late step only joins the
// timeline.
func (s *Shipping) report(ctx context.Context, c, code string, evs []carrier.Event) error {
	stepped, err := s.keep(ctx, c, code, evs)
	if err != nil || stepped.IsZero() {
		return err
	}
	t, err := s.Tracking(ctx, c, code, false)
	if err != nil {
		return err
	}
	st, moves := t.Step.Status()
	if !moves || t.Events[0].At.After(stepped) {
		return nil
	}
	err = s.d.Deals.ShipmentUpdated(ctx, code, string(st))
	if err != nil && !errors.Is(err, s.d.Deals.NotFound) {
		return fmt.Errorf("%s reported %s %s: %w", c, code, st, err)
	}
	return nil
}

// keep stores evs and returns when the newest new step among them was,
// zero for none.
func (s *Shipping) keep(ctx context.Context, c, code string, evs []carrier.Event) (stepped time.Time, err error) {
	for i := range evs {
		ev := &evs[i]
		kept, addErr := s.d.Events.Add(ctx, c, code, *ev)
		if addErr != nil {
			return time.Time{}, fmt.Errorf("keep %s %s: %w", c, code, addErr)
		}
		if kept && ev.Kind == carrier.KindStep && ev.At.After(stepped) {
			stepped = ev.At
		}
	}
	return stepped, nil
}

func (s *Shipping) Tracking(ctx context.Context, c, code string, seller bool) (Tracking, error) {
	evs, err := s.d.Events.List(ctx, c, code, !seller)
	if err != nil {
		return Tracking{}, fmt.Errorf("read the events of %s %s: %w", c, code, err)
	}
	t := Tracking{Events: evs}
	for i := range evs {
		if evs[i].Kind == carrier.KindStep {
			t.Step = evs[i].Step
			break
		}
	}
	return t, nil
}

func (s *Shipping) AccountDeleted(ctx context.Context, owner uuid.UUID) error {
	if err := s.d.Accounts.DeleteAll(ctx, owner); err != nil {
		return fmt.Errorf("drop the carrier accounts: %w", err)
	}
	return nil
}
