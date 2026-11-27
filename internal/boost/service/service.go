// Package service is module boost's use cases: a seller buys a package
// for a listing, its payment lands or closes, and its timer moves the
// listing up once a day until the package ends.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"
	"uuid"

	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/boost/domain"
	"github.com/shopnexus/server/internal/boost/repository"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	paymentdomain "github.com/shopnexus/server/internal/payment/domain"
	paymentsvc "github.com/shopnexus/server/internal/payment/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
)

// View is a boost as its seller reads it. PaymentURL and PaymentExpiresAt
// are there while it awaits payment.
type View struct {
	ID               uuid.UUID     `json:"id"`
	ListingID        uuid.UUID     `json:"listing_id"`
	SellerID         uuid.UUID     `json:"-"`
	Package          string        `json:"package"`
	Days             int           `json:"days"`
	Price            int64         `json:"price"`
	Status           domain.Status `json:"status"`
	PaymentURL       string        `json:"payment_url,omitempty"`
	PaymentExpiresAt *time.Time    `json:"payment_expires_at,omitempty"`
	StartedAt        *time.Time    `json:"started_at,omitempty"`
	EndsAt           *time.Time    `json:"ends_at,omitempty"`
	NextBumpAt       *time.Time    `json:"next_bump_at,omitempty"`
	LastBumpAt       *time.Time    `json:"last_bump_at,omitempty"`
	Bumps            int           `json:"bumps"`
	EndedAt          *time.Time    `json:"ended_at,omitempty"`
	RefundedAt       *time.Time    `json:"refunded_at,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
}

// Packages is what GET /v1/boost-packages answers.
type Packages struct {
	Items     []domain.Package
	Available bool
}

// BuyInput is a purchase: the package for the seller's listing, under the
// app's key, from IP, the payment page in English or not.
type BuyInput struct {
	Seller    uuid.UUID
	ListingID uuid.UUID
	Package   string
	Key       uuid.UUID
	IP        string
	English   bool
}

// Standing is one of a seller's listings as boosting sees it: its boost
// awaiting payment or running, and whether Buy would sell it a package now.
type Standing struct {
	Boost     *View
	Boostable bool
}

// ConsoleBoost is a boost with its payment, as staff read them.
type ConsoleBoost struct {
	Boost   View
	Payment paymentsvc.View
}

// Service is module boost.
type Service interface {
	// Packages lists the packages, shortest first.
	Packages() Packages
	// Buy is UC-LST-03; created is false for a boost it found instead.
	Buy(ctx context.Context, in BuyInput) (v View, created bool, err error)
	// Get reads one of the seller's boosts; domain.ErrNotFound for another.
	Get(ctx context.Context, seller, id uuid.UUID) (View, error)
	// Boost reads any boost, for notification.
	Boost(ctx context.Context, id uuid.UUID) (View, error)
	// Standings reads the Standing of each of the seller's listings, given
	// with its status, keyed by listing; another seller's boosts are left out.
	Standings(ctx context.Context, seller uuid.UUID, listings map[uuid.UUID]catalogdomain.Status) (map[uuid.UUID]Standing, error)
	// ListingBoosts lists a listing's boosts with their payments, newest
	// first, for the console.
	ListingBoosts(ctx context.Context, listing uuid.UUID, p page.Request) (page.Page[ConsoleBoost], error)
	// PaymentPaid starts the boost the payment paid for, or refunds it.
	PaymentPaid(ctx context.Context, payment uuid.UUID, at time.Time, writeID, eventID uuid.UUID) error
	// PaymentClosed closes the boost of a payment that failed or expired.
	PaymentClosed(ctx context.Context, payment uuid.UUID, status domain.Status, at time.Time, writeID, eventID uuid.UUID) error
	// PaymentRefunded records the refund of a boost's payment.
	PaymentRefunded(ctx context.Context, payment uuid.UUID, at time.Time, writeID, eventID uuid.UUID) error
}

// Catalog is what boost needs of module catalog.
type Catalog interface {
	Summary(ctx context.Context, id uuid.UUID) (catalogsvc.Summary, bool, error)
	Bump(ctx context.Context, seller, id uuid.UUID, at time.Time) (bool, error)
}

// Deps is what the service needs.
type Deps struct {
	Store    repository.Store
	Packages []domain.Package
	Payments paymentsvc.Service
	Catalog  Catalog
	Accounts accountsvc.Service
	Ingress  *ingress.Client
	Prefix   string
	Now      func() time.Time
	Log      *slog.Logger
}

// Boosts implements Service.
type Boosts struct{ d Deps }

var _ Service = (*Boosts)(nil)

func New(d Deps) *Boosts { return &Boosts{d: d} }

func (s *Boosts) Packages() Packages {
	return Packages{Items: s.d.Packages, Available: s.d.Payments.Available()}
}

func (s *Boosts) pkg(id string) (domain.Package, bool) {
	for _, p := range s.d.Packages {
		if p.ID == id {
			return p, true
		}
	}
	return domain.Package{}, false
}

// view is b as its seller reads it, with its payment when it awaits one.
func view(b domain.Boost, p *paymentsvc.View) View {
	st := b.State()
	v := View{
		ID: st.ID, ListingID: st.ListingID, SellerID: st.SellerID, Package: st.Package, Days: st.Days, Price: st.Price,
		Status: st.Status, StartedAt: st.StartedAt, EndsAt: st.EndsAt, LastBumpAt: st.LastBumpAt, Bumps: st.Bumps,
		EndedAt: st.EndedAt, RefundedAt: st.RefundedAt, CreatedAt: st.CreatedAt,
	}
	if st.NextTickAt != nil && st.EndsAt != nil && st.NextTickAt.Before(*st.EndsAt) {
		v.NextBumpAt = st.NextTickAt
	}
	if st.Status == domain.StatusAwaiting && p != nil {
		v.PaymentURL, v.PaymentExpiresAt = p.PayURL, &p.ExpiresAt
	}
	return v
}

// views reads the payments of the boosts that await one.
func (s *Boosts) views(ctx context.Context, bs []domain.Boost) ([]View, error) {
	var ids []uuid.UUID
	for i := range bs {
		if bs[i].Status() == domain.StatusAwaiting {
			ids = append(ids, bs[i].PaymentID())
		}
	}
	ps, err := s.d.Payments.Many(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read boosts' payments: %w", err)
	}
	out := make([]View, len(bs))
	for i := range bs {
		var p *paymentsvc.View
		if got, ok := ps[bs[i].PaymentID()]; ok {
			p = &got
		}
		out[i] = view(bs[i], p)
	}
	return out, nil
}

func (s *Boosts) one(ctx context.Context, b domain.Boost) (View, error) {
	vs, err := s.views(ctx, []domain.Boost{b})
	if err != nil {
		return View{}, err
	}
	return vs[0], nil
}

func (s *Boosts) Get(ctx context.Context, seller, id uuid.UUID) (View, error) {
	b, err := s.d.Store.Get(ctx, id)
	if err != nil {
		return View{}, fmt.Errorf("read boost %s: %w", id, err)
	}
	if b.SellerID() != seller {
		return View{}, domain.ErrNotFound
	}
	return s.one(ctx, b)
}

func (s *Boosts) Boost(ctx context.Context, id uuid.UUID) (View, error) {
	b, err := s.d.Store.Get(ctx, id)
	if err != nil {
		return View{}, fmt.Errorf("read boost %s: %w", id, err)
	}
	return view(b, nil), nil
}

func (s *Boosts) Standings(ctx context.Context, seller uuid.UUID, listings map[uuid.UUID]catalogdomain.Status) (map[uuid.UUID]Standing, error) {
	live, err := s.live(ctx, seller, slices.Collect(maps.Keys(listings)))
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]Standing, len(listings))
	open := false
	for id, st := range listings {
		var sd Standing
		if v, ok := live[id]; ok {
			sd.Boost = &v
		} else {
			sd.Boostable = onSale(st)
		}
		open = open || sd.Boostable
		out[id] = sd
	}
	if !open {
		return out, nil
	}
	// Some listing passes; Buy's checks of the server and the seller decide.
	var refused error = paymentdomain.ErrUnavailable
	if s.d.Payments.Available() {
		if refused, err = s.sellerRefusal(ctx, seller); err != nil {
			return nil, err
		}
	}
	if refused != nil {
		for id, sd := range out {
			sd.Boostable = false
			out[id] = sd
		}
	}
	return out, nil
}

// live reads the boost awaiting payment or running on each of the seller's
// listings, keyed by listing.
func (s *Boosts) live(ctx context.Context, seller uuid.UUID, listings []uuid.UUID) (map[uuid.UUID]View, error) {
	live, err := s.d.Store.Live(ctx, listings)
	if err != nil {
		return nil, fmt.Errorf("read the live boosts: %w", err)
	}
	var bs []domain.Boost
	for id := range live {
		if b := live[id]; b.SellerID() == seller {
			bs = append(bs, b)
		}
	}
	vs, err := s.views(ctx, bs)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]View, len(vs))
	for i := range vs {
		out[vs[i].ListingID] = vs[i]
	}
	return out, nil
}

func (s *Boosts) ListingBoosts(ctx context.Context, listing uuid.UUID, p page.Request) (page.Page[ConsoleBoost], error) {
	bs, err := s.d.Store.OfListing(ctx, listing, p)
	if err != nil {
		return page.Page[ConsoleBoost]{}, fmt.Errorf("read the listing's boosts: %w", err)
	}
	ids := make([]uuid.UUID, len(bs))
	for i := range bs {
		ids[i] = bs[i].PaymentID()
	}
	ps, err := s.d.Payments.Many(ctx, ids)
	if err != nil {
		return page.Page[ConsoleBoost]{}, fmt.Errorf("read boosts' payments: %w", err)
	}
	rows := make([]ConsoleBoost, len(bs))
	for i := range bs {
		pay := ps[bs[i].PaymentID()]
		rows[i] = ConsoleBoost{Boost: view(bs[i], &pay), Payment: pay}
	}
	return page.Of(rows, p, func(c ConsoleBoost) uuid.UUID { return c.Boost.ID }), nil
}

// saveTries bounds the reads again after a save that lost a race.
const saveTries = 5

// change reads the boost of a payment, lets f change it, and saves it
// under writeID with an event of the kind f returns; no kind saves
// nothing. A save that lost a race reads again.
func (s *Boosts) change(ctx context.Context, payment, writeID, eventID uuid.UUID, at time.Time,
	f func(b *domain.Boost) (events.Kind, error),
) (domain.Boost, error) {
	for range saveTries {
		b, err := s.d.Store.ByPayment(ctx, payment)
		if err != nil {
			return domain.Boost{}, fmt.Errorf("read the boost of payment %s: %w", payment, err)
		}
		kind, err := f(&b)
		if err != nil || kind == "" {
			return b, err
		}
		var evs []events.Event
		if kind != kindNone {
			evs = append(evs, events.Event{ID: eventID, Kind: kind, SubjectID: b.ID(), OccurredAt: at})
		}
		err = s.d.Store.Save(ctx, &b, writeID, evs...)
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		if err != nil {
			return domain.Boost{}, fmt.Errorf("save boost %s: %w", b.ID(), err)
		}
		return b, nil
	}
	return domain.Boost{}, fmt.Errorf("save the boost of payment %s: %w", payment, repository.ErrConflict)
}

// kindNone is a change f saves without an event.
const kindNone events.Kind = "-"
