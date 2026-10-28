package service_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountrepo "github.com/shopnexus/server2/internal/account/repository"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authdomain "github.com/shopnexus/server2/internal/auth/domain"
	authrepo "github.com/shopnexus/server2/internal/auth/repository"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	dealrepo "github.com/shopnexus/server2/internal/deal/repository"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	mediarepo "github.com/shopnexus/server2/internal/media/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/trust/repository"
	"github.com/shopnexus/server2/internal/trust/service"
)

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	listings catalogrepo.Listings
	deals    *dealsvc.Deals
	trust    *service.Trust
}

// newEnv builds trust and the modules it calls on a database, and serves
// deal's flows so orders can be shipped.
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
	deals := dealsvc.New(dealsvc.Deps{
		Deals: dealrepo.NewDeals(db), Accounts: accounts, Catalog: c, Ingress: in, Prefix: p, Now: time.Now,
	})
	restatetest.Start(t, deals.Durable()...)
	trust := service.New(service.Deps{Reviews: repository.NewReviews(db), Accounts: accounts, Catalog: c, Deals: deals, Now: time.Now})
	return env{db: db, accounts: accounts, listings: listings, deals: deals, trust: trust}
}

// person creates an account with a verified phone number.
func (e env) person(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if err := e.accounts.Create(context.Background(), id, "Linh"); err != nil {
		t.Fatal(err)
	}
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

// listing puts a listing of seller's on sale and returns its ID.
func (e env) listing(t *testing.T, seller uuid.UUID) uuid.UUID {
	t.Helper()
	photo := uuid.NewV7()
	now := time.Now()
	l, err := catalogdomain.NewListing(uuid.NewV7(), seller, catalogdomain.Draft{
		Title: "Xe đạp Asama", Price: 1_500_000, CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed,
		CommuneCode: "00004", Photos: []uuid.UUID{photo},
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

// requested agrees a deal between buyer and seller and asks for COD,
// returning the deal and its order.
func (e env) requested(t *testing.T, buyer, seller uuid.UUID) (deal, order uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	d, _, err := e.deals.Open(ctx, buyer, e.listing(t, seller))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.deals.Accept(ctx, buyer, d.ID, 1_500_000); err != nil {
		t.Fatal(err)
	}
	a, err := e.accounts.CreateAddress(ctx, buyer, uuid.NewV7(), accountdomain.AddressDraft{
		Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004",
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.deals.RequestCod(ctx, buyer, d.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return d.ID, v.Order.ID
}

// shipped is a requested order the seller shipped.
func (e env) shipped(t *testing.T, buyer, seller uuid.UUID) (deal, order uuid.UUID) {
	t.Helper()
	deal, order = e.requested(t, buyer, seller)
	if _, err := e.deals.Ship(context.Background(), seller, deal, uuid.NewV7().String(), dealsvc.WaybillInput{
		Carrier: "spx", TrackingCode: "SPX1", Fee: 22_000,
	}); err != nil {
		t.Fatal(err)
	}
	return deal, order
}

func (e env) suspend(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := e.accounts.Suspend(context.Background(), accountsvc.SuspendInput{ID: id, Reason: "spam", By: audit.ByCLI("test")}); err != nil {
		t.Fatal(err)
	}
}
