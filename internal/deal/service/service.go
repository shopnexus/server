// Package service holds module deal's use cases: a buyer opens a deal on a
// listing, the two sides move until they agree a price, and the deals
// follow their listings' changes.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// Service is module deal's contract.
//
//nolint:interfacebloat // One method per use case of deals and their orders; splitting it would split the module.
type Service interface {
	// Open returns the buyer's deal on a listing, starting one when there is
	// none; created reports a new deal.
	Open(ctx context.Context, buyer, listingID uuid.UUID) (v DealView, created bool, err error)
	// Get reads a deal for one of its two sides.
	Get(ctx context.Context, viewer, id uuid.UUID) (DealView, error)
	// ListMine lists viewer's deals on side as, newest first.
	ListMine(ctx context.Context, viewer uuid.UUID, as domain.Party, p page.Request) (page.Page[DealView], error)
	Offer(ctx context.Context, caller, id uuid.UUID, amount int64) (DealView, error)
	Accept(ctx context.Context, caller, id uuid.UUID, price int64) (DealView, error)
	Decline(ctx context.Context, caller, id uuid.UUID) (DealView, error)
	Withdraw(ctx context.Context, caller, id uuid.UUID) (DealView, error)
	Cancel(ctx context.Context, caller, id uuid.UUID) (DealView, error)
	// RequestCod asks for cash on delivery on the buyer's agreed deal.
	RequestCod(ctx context.Context, caller, id uuid.UUID, r RecipientInput) (DealView, error)
	// Ship records the waybill of a requested order, which holds the
	// listing; Deliver and Return end a shipped one. A repeated
	// idempotencyKey of the same caller runs nothing more.
	Ship(ctx context.Context, caller, id uuid.UUID, idempotencyKey string, w WaybillInput) (DealView, error)
	Deliver(ctx context.Context, caller, id uuid.UUID, idempotencyKey string) (DealView, error)
	Return(ctx context.Context, caller, id uuid.UUID, idempotencyKey string) (DealView, error)
	// ListOrders lists viewer's orders on side as, newest first.
	ListOrders(ctx context.Context, viewer uuid.UUID, as domain.Party, p page.Request) (page.Page[OrderItem], error)
	// ListingChanged brings a listing's deals in line with its current
	// state; the worker calls it for every change of the listing.
	ListingChanged(ctx context.Context, listingID uuid.UUID) error
}

// Deps is what New builds Deals from.
type Deps struct {
	Deals    repository.Deals
	Accounts accountsvc.Service
	Auth     authsvc.Service
	Catalog  catalogsvc.Service
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
}

// Deals implements Service in this process.
type Deals struct {
	deals    repository.Deals
	accounts accountsvc.Service
	auth     authsvc.Service
	catalog  catalogsvc.Service
	prefix   string
	now      func() time.Time
	ship     restatex.Caller[FlowInput, FlowResult]
	finish   restatex.Caller[FlowInput, FlowResult]
}

var _ Service = (*Deals)(nil)

func New(d Deps) *Deals {
	return &Deals{
		deals: d.Deals, accounts: d.Accounts, auth: d.Auth, catalog: d.Catalog, prefix: d.Prefix, now: d.Now,
		ship:   restatex.Caller[FlowInput, FlowResult]{Ingress: d.Ingress, Service: d.Prefix + dealShip, Handler: "Run"},
		finish: restatex.Caller[FlowInput, FlowResult]{Ingress: d.Ingress, Service: d.Prefix + dealFinish, Handler: "Run"},
	}
}

// Durable returns module deal's Restate services, for Mount to bind.
func (s *Deals) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{
		restate.Reflect(DealShip{Deals: s}), restate.Reflect(DealFinish{Deals: s}),
	}
}

// refuseSuspended returns accountdomain.ErrSuspended for a suspended
// account.
func (s *Deals) refuseSuspended(ctx context.Context, id uuid.UUID) error {
	st, err := s.accounts.Status(ctx, id)
	if err != nil {
		return fmt.Errorf("read account status: %w", err)
	}
	if st == accountdomain.StatusSuspended {
		return accountdomain.ErrSuspended
	}
	return nil
}

// listing reads one listing through catalog, in any status; an unknown
// one is catalogdomain.ErrNotFound.
func (s *Deals) listing(ctx context.Context, id uuid.UUID) (catalogsvc.Summary, error) {
	got, err := s.catalog.Summaries(ctx, []uuid.UUID{id})
	if err != nil {
		return catalogsvc.Summary{}, fmt.Errorf("read listing: %w", err)
	}
	l, ok := got[id]
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

// moved is the event of a deal's new moves.
func moved(id uuid.UUID, now time.Time) events.Event {
	return events.Event{ID: uuid.NewV7(), Kind: events.DealMoved, SubjectID: id, OccurredAt: now}
}
