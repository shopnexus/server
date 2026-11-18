package service_test

import (
	"context"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatrepo "github.com/shopnexus/server/internal/chat/repository"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/provider/carrier"
	shippingdomain "github.com/shopnexus/server/internal/shipping/domain"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
)

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	catalog  *catalogsvc.Catalog
	chats    *chatsvc.Chat
	listings catalogrepo.Listings
	repo     repository.Deals
	deps     service.Deps
	deals    *service.Deals
}

// newEnv builds the deal service on a database, with the account and
// catalog services it calls, and serves its flows on the test stack. deps
// lets a test build a second service with one dependency replaced.
func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	media := mediatest.Service(db, objectstoretest.New(t), in, p)
	c := catalogtest.Service(db, accounts, media, in, p)
	chats := chatsvc.New(chatsvc.Deps{
		Chats: chatrepo.NewChats(db), Accounts: accounts, Catalog: c, Media: media, Ingress: in, Prefix: p, Now: time.Now,
	})
	repo := repository.NewDeals(db)
	deps := service.Deps{
		Deals: repo, Accounts: accounts, Catalog: c, Chats: chats, Shipping: linkedAt{}, Waits: domain.OrderWaits, Ingress: in,
		Now: time.Now, Log: slog.New(slog.DiscardHandler),
	}
	return env{
		db: db, accounts: accounts, catalog: c, chats: chats, listings: catalogrepo.NewListings(db), repo: repo, deps: deps,
		deals: start(t, deps),
	}
}

// start builds a deal service on deps under a prefix of its own and serves
// its flows.
func start(t *testing.T, deps service.Deps) *service.Deals {
	t.Helper()
	deps.Prefix = restatetest.Prefix()
	d := service.New(deps)
	restatetest.Start(t, d.Durable()...)
	return d
}

// person creates an account with a verified phone number.
func (e env) person(t *testing.T) uuid.UUID {
	t.Helper()
	return accounttest.WithPhone(t, e.db)
}

// edit changes a listing's draft and saves it, as an edit with the same
// photos does: the listing stays on sale.
func (e env) edit(t *testing.T, id uuid.UUID, change func(*catalogdomain.ListingInput)) {
	t.Helper()
	ctx := context.Background()
	l, err := e.listings.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	d := catalogdomain.ListingInput{
		Title: l.Title(), Price: l.Price(), CategoryCode: l.CategoryCode(), Condition: l.Condition(),
		AllowOffers: l.AllowOffers(), CommuneCode: l.CommuneCode(), Photos: l.Uploads(),
	}
	change(&d)
	if err = l.Edit(d, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = e.listings.Save(ctx, &l, uuid.NewV7(), nil); err != nil {
		t.Fatal(err)
	}
}

// open opens buyer's deal on listing.
func (e env) open(t *testing.T, buyer, listing uuid.UUID) service.DealView {
	t.Helper()
	v, _, err := e.deals.Open(context.Background(), buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// get reads deal id as viewer.
func (e env) get(t *testing.T, viewer, id uuid.UUID) service.DealView {
	t.Helper()
	v, err := e.deals.Get(context.Background(), viewer, id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// moveKinds lists a view's move kinds.
func moveKinds(v service.DealView) []domain.MoveKind {
	out := make([]domain.MoveKind, 0, len(v.Moves))
	for _, m := range v.Moves {
		out = append(out, m.Kind)
	}
	return out
}

// must fails t on an error from a setup call: must(t)(e.deals.Offer(...)).
func must(t *testing.T) func(service.DealView, error) {
	t.Helper()
	return func(_ service.DealView, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// staleGet is a store whose Get returns a deal read earlier, as a request
// that loaded the deal before the other side moved.
type staleGet struct {
	repository.Deals

	stale domain.Deal
}

func (s staleGet) Get(context.Context, uuid.UUID) (domain.Deal, error) { return s.stale, nil }

// linkedAt is module shipping for the tests: every seller has linked every
// carrier, and c answers for them all; a nil c is the fake of the carrier
// asked.
type linkedAt struct{ c carrier.Client }

func (l linkedAt) client(c carrier.Carrier) carrier.Client {
	if l.c != nil {
		return l.c
	}
	return carrier.Fake{Carrier: c}
}

func (l linkedAt) Create(ctx context.Context, _ uuid.UUID, c carrier.Carrier, in carrier.CreateInput) (carrier.Created, error) {
	return l.client(c).Create(ctx, carrier.Account{}, in)
}

func (l linkedAt) Lookup(ctx context.Context, _ uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error) {
	return l.client(c).Lookup(ctx, carrier.Account{}, code)
}

func (l linkedAt) Cancel(ctx context.Context, _ uuid.UUID, c carrier.Carrier, code string) error {
	return l.client(c).Cancel(ctx, carrier.Account{}, code)
}

// refusing is module shipping answering every call with err: a stack
// without a carrier, or a seller who linked none.
type refusing struct{ err error }

func (r refusing) Create(context.Context, uuid.UUID, carrier.Carrier, carrier.CreateInput) (carrier.Created, error) {
	return carrier.Created{}, r.err
}

func (r refusing) Lookup(context.Context, uuid.UUID, carrier.Carrier, string) (carrier.Parcel, error) {
	return carrier.Parcel{}, r.err
}

func (r refusing) Cancel(context.Context, uuid.UUID, carrier.Carrier, string) error { return r.err }

// errShippingUnavailable is module shipping's answer with CARRIER=none; it
// shares its code with deal's error.
var errShippingUnavailable = apperr.New(apperr.KindUnavailable, "carrier_unavailable", "no carrier makes or reads shipments here")

// errShippingNotLinked is module shipping's answer for a carrier the seller
// has not linked.
var errShippingNotLinked = apperr.New(apperr.KindConflict, "carrier_not_linked", "the caller has not linked that carrier")

func (linkedAt) SavedPickup(context.Context, uuid.UUID) (shippingsvc.Pickup, error) {
	return shippingsvc.Pickup{}, shippingdomain.ErrPickupNotConfigured
}

func (r refusing) SavedPickup(context.Context, uuid.UUID) (shippingsvc.Pickup, error) {
	return shippingsvc.Pickup{}, r.err
}

func (*recording) SavedPickup(context.Context, uuid.UUID) (shippingsvc.Pickup, error) {
	return shippingsvc.Pickup{}, shippingdomain.ErrPickupNotConfigured
}
