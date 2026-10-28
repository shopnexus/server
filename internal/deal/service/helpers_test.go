package service_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	accountrepo "github.com/shopnexus/server2/internal/account/repository"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authdomain "github.com/shopnexus/server2/internal/auth/domain"
	authrepo "github.com/shopnexus/server2/internal/auth/repository"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/deal/service"
	mediarepo "github.com/shopnexus/server2/internal/media/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/contact"
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
	url, _, err := restatetest.URLs()
	if err != nil {
		t.Fatal(err)
	}
	in, p := ingress.NewClient(url), restatetest.Prefix()
	accounts := accountsvc.New(accountrepo.NewAccounts(db), nil, time.Now)
	media := mediasvc.New(mediasvc.Deps{
		Uploads: mediarepo.NewUploads(db), Store: objectstoretest.New(t), Ingress: in, Prefix: p, Now: time.Now,
	})
	listings := catalogrepo.NewListings(db)
	c := catalogsvc.New(catalogsvc.Deps{
		Listings: listings, Categories: catalogrepo.NewCategories(db), Regions: catalogrepo.NewRegions(db),
		Accounts: accounts, Media: media, Ingress: in, Prefix: p, Now: time.Now,
	})
	repo := repository.NewDeals(db)
	deps := service.Deps{Deals: repo, Accounts: accounts, Catalog: c, Ingress: in, Now: time.Now}
	return env{db: db, accounts: accounts, catalog: c, listings: listings, repo: repo, deps: deps, deals: start(t, deps)}
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
	id := e.phoneless(t)
	phone, err := contact.ParsePhone(fmt.Sprintf("09%08d", rand.IntN(100_000_000)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	creds := authdomain.NewCredentials(id, authdomain.NewIdentity(uuid.NewV7(), phone, now), now)
	if err = authrepo.NewCredentials(e.db).Create(context.Background(), creds); err != nil {
		t.Fatal(err)
	}
	return id
}

// phoneless creates an account with no identity.
func (e env) phoneless(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if err := e.accounts.Create(context.Background(), id, "Linh"); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e env) suspend(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := e.accounts.Suspend(context.Background(), accountsvc.SuspendInput{
		ID: id, Reason: "spam", By: audit.ByCLI("test"),
	}); err != nil {
		t.Fatal(err)
	}
}

// listing puts a listing of seller's on sale at 1,500,000 đ, taking
// offers, its one photo processed, and returns its ID.
func (e env) listing(t *testing.T, seller uuid.UUID) uuid.UUID {
	t.Helper()
	photo := uuid.NewV7()
	now := time.Now()
	l, err := catalogdomain.NewListing(uuid.NewV7(), seller, catalogdomain.Draft{
		Title: "Xe đạp Asama", Price: 1_500_000, CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed,
		AllowOffers: true, CommuneCode: "00004", Photos: []uuid.UUID{photo},
	}, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	l.Published(map[uuid.UUID]catalogdomain.PhotoKeys{photo: {Photo: "media/a.jpg", Thumb: "media/a_thumb.jpg"}}, now)
	if err = e.listings.Insert(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return l.ID()
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

// kindsOf lists the kinds of the events about subject, oldest first.
func kindsOf(t *testing.T, db *pgxpool.Pool, subject uuid.UUID) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT kind FROM events WHERE subject_id = @id ORDER BY id`, pgx.StrictNamedArgs{"id": subject})
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return kinds
}

// staleGet is a store whose Get returns a deal read earlier, as a request
// that loaded the deal before the other side moved.
type staleGet struct {
	repository.Deals

	stale domain.Deal
}

func (s staleGet) Get(context.Context, uuid.UUID) (domain.Deal, error) { return s.stale, nil }
