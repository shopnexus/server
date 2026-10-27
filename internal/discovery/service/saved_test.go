package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	accountrepo "github.com/shopnexus/server2/internal/account/repository"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authrepo "github.com/shopnexus/server2/internal/auth/repository"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	mediarepo "github.com/shopnexus/server2/internal/media/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

type env struct {
	db        *pgxpool.Pool
	listings  catalogrepo.Listings
	catalog   *catalogsvc.Catalog
	discovery *service.Discovery
}

func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	url, _, err := restatetest.URLs()
	if err != nil {
		t.Fatal(err)
	}
	in, p := ingress.NewClient(url), restatetest.Prefix()
	accounts := accountsvc.New(accountrepo.NewAccounts(db), nil, time.Now)
	auth := authsvc.New(authsvc.Deps{Credentials: authrepo.NewCredentials(db)})
	media := mediasvc.New(mediasvc.Deps{
		Uploads: mediarepo.NewUploads(db), Store: objectstoretest.New(t), Ingress: in, Prefix: p, Now: time.Now,
	})
	listings := catalogrepo.NewListings(db)
	c := catalogsvc.New(catalogsvc.Deps{
		Listings: listings, Categories: catalogrepo.NewCategories(db), Regions: catalogrepo.NewRegions(db),
		Accounts: accounts, Auth: auth, Media: media, Ingress: in, Prefix: p, Now: time.Now,
	})
	d := service.New(service.Deps{Saves: repository.NewSaves(db), Catalog: c, Now: time.Now})
	return env{db: db, listings: listings, catalog: c, discovery: d}
}

func (e env) account(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := e.db.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

// listing puts a listing of seller's on sale at price and returns its ID.
func (e env) listing(t *testing.T, seller uuid.UUID, price int64) uuid.UUID {
	t.Helper()
	photo := uuid.NewV7()
	now := time.Now()
	l, err := catalogdomain.NewListing(uuid.NewV7(), seller, catalogdomain.Draft{
		Title: "Xe đạp Asama", Price: price, CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed,
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

func TestSaveAndList(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, me := e.account(t), e.account(t)
	listing := e.listing(t, seller, 1_500_000)
	if err := e.discovery.Save(ctx, me, listing); err != nil {
		t.Fatal(err)
	}
	reserved := e.listing(t, seller, 900_000)
	if err := e.catalog.Reserve(ctx, reserved, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err := e.discovery.Save(ctx, me, reserved); err != nil {
		t.Fatalf("a reserved listing: %v", err)
	}
	p, err := e.discovery.Saved(ctx, me, page.Request{})
	if err != nil || len(p.Items) != 2 || p.Items[1].ListingID != listing || p.Items[1].SavedPrice != 1_500_000 ||
		p.Items[1].Listing.Title == "" || p.Items[1].Listing.CoverThumbURL == "" || p.Items[0].Listing.Status != catalogdomain.StatusReserved {
		t.Fatalf("%+v %v", p, err)
	}
	if err = e.discovery.Unsave(ctx, me, reserved); err != nil {
		t.Fatal(err)
	}
	if p, _ = e.discovery.Saved(ctx, me, page.Request{}); len(p.Items) != 1 {
		t.Fatalf("after unsave: %+v", p)
	}
}

func TestSaveRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, me := e.account(t), e.account(t)
	listing := e.listing(t, seller, 1_500_000)
	if err := e.discovery.Save(ctx, seller, listing); !errors.Is(err, domain.ErrOwnListing) {
		t.Errorf("own: %v", err)
	}
	if err := e.discovery.Save(ctx, me, uuid.NewV7()); !errors.Is(err, catalogdomain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	hidden := e.listing(t, seller, 1_500_000)
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	if err := e.discovery.Save(ctx, me, hidden); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Errorf("hidden: %v", err)
	}
}

func TestHiddenSaveShowsStatusOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, me := e.account(t), e.account(t)
	listing := e.listing(t, seller, 1_500_000)
	if err := e.discovery.Save(ctx, me, listing); err != nil {
		t.Fatal(err)
	}
	if _, err := e.catalog.Hide(ctx, seller, listing); err != nil {
		t.Fatal(err)
	}
	p, err := e.discovery.Saved(ctx, me, page.Request{})
	if err != nil || len(p.Items) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	l := p.Items[0].Listing
	if l.Status != catalogdomain.StatusHidden || l.Title != "" || l.Price != nil || l.CoverThumbURL != "" {
		t.Fatalf("%+v", l)
	}
}

func TestIsSaved(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, me := e.account(t), e.account(t)
	listing := e.listing(t, seller, 1_500_000)
	if saved, err := e.discovery.IsSaved(ctx, me, listing); err != nil || saved {
		t.Fatalf("before: %v %v", saved, err)
	}
	if err := e.discovery.Save(ctx, me, listing); err != nil {
		t.Fatal(err)
	}
	if saved, err := e.discovery.IsSaved(ctx, me, listing); err != nil || !saved {
		t.Fatalf("after: %v %v", saved, err)
	}
	if err := e.discovery.Unsave(ctx, me, listing); err != nil {
		t.Fatal(err)
	}
	if saved, err := e.discovery.IsSaved(ctx, me, listing); err != nil || saved {
		t.Fatalf("after unsave: %v %v", saved, err)
	}
}
