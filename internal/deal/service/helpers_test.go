package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/media/mediatest"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	catalog  *catalogsvc.Catalog
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
	c := catalogtest.Service(db, accounts, mediatest.Service(db, objectstoretest.New(t), in, p), in, p)
	repo := repository.NewDeals(db)
	deps := service.Deps{Deals: repo, Accounts: accounts, Catalog: c, Ingress: in, Now: time.Now}
	return env{db: db, accounts: accounts, catalog: c, listings: catalogrepo.NewListings(db), repo: repo, deps: deps, deals: start(t, deps)}
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
func (e env) edit(t *testing.T, id uuid.UUID, change func(*catalogdomain.Draft)) {
	t.Helper()
	ctx := context.Background()
	l, err := e.listings.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	d := catalogdomain.Draft{
		Title: l.Title(), Price: l.Price(), CategoryCode: l.CategoryCode(), Condition: l.Condition(),
		AllowOffers: l.AllowOffers(), CommuneCode: l.CommuneCode(), Photos: l.Uploads(),
	}
	change(&d)
	if err = l.Edit(d, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = e.listings.Save(ctx, &l, uuid.NewV7()); err != nil {
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
