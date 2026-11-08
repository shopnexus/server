package service_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/media/mediatest"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

type env struct {
	db        *pgxpool.Pool
	catalog   *catalogsvc.Catalog
	discovery *service.Discovery
	model     *fakeModel
}

func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	c := catalogtest.Service(db, accounttest.Service(db), mediatest.Service(db, objectstoretest.New(t), in, p), in, p)
	model := &fakeModel{answers: map[string]string{}}
	d := service.New(service.Deps{
		Saves: repository.NewSaves(db), Understandings: repository.NewUnderstandings(db), Catalog: c, LLM: model,
		ModelTimeout: 200 * time.Millisecond, Log: slog.New(slog.DiscardHandler), Now: time.Now,
	})
	return env{db: db, catalog: c, discovery: d, model: model}
}

func TestSaveAndList(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, me := accounttest.New(t, e.db), accounttest.New(t, e.db)
	listing := catalogtest.Listing(t, e.db, seller)
	if err := e.discovery.Save(ctx, me, listing); err != nil {
		t.Fatal(err)
	}
	reserved := catalogtest.Listing(t, e.db, seller, func(d *catalogdomain.Draft, _ *catalogdomain.Refs) { d.Price = 900_000 })
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
	seller, me := accounttest.New(t, e.db), accounttest.New(t, e.db)
	listing := catalogtest.Listing(t, e.db, seller)
	if err := e.discovery.Save(ctx, seller, listing); !errors.Is(err, domain.ErrOwnListing) {
		t.Errorf("own: %v", err)
	}
	if err := e.discovery.Save(ctx, me, uuid.NewV7()); !errors.Is(err, catalogdomain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	hidden := catalogtest.Listing(t, e.db, seller)
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
	seller, me := accounttest.New(t, e.db), accounttest.New(t, e.db)
	listing := catalogtest.Listing(t, e.db, seller)
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
	seller, me := accounttest.New(t, e.db), accounttest.New(t, e.db)
	listing := catalogtest.Listing(t, e.db, seller)
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
