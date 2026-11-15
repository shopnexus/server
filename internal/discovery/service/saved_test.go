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
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/media/mediatest"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
)

type env struct {
	db          *pgxpool.Pool
	catalog     *catalogsvc.Catalog
	media       *mediasvc.Media
	discovery   *service.Discovery
	model       *fakeModel
	text, image *embedtest.Model
	accounts    *accountsvc.Account
	sweeps      *sweeps
}

// newEnv builds discovery on a database; each option changes its
// dependencies, to put a faulty store in.
func newEnv(t *testing.T, opts ...func(*service.Deps)) env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	model := &fakeModel{answers: map[string]string{}}
	m := mediatest.Service(db, objectstoretest.New(t), in, p)
	restatetest.Start(t, m.Durable()...)
	acc, sw := accounttest.Service(db), &sweeps{}
	c := catalogtest.Service(db, acc, m, in, p)
	text, image := embedtest.New(catalogdomain.TextDims), embedtest.New(catalogdomain.PhotoDims)
	deps := service.Deps{
		Saves: repository.NewSaves(db), Understandings: repository.NewUnderstandings(db), History: repository.NewHistory(db),
		Hidden: repository.NewHidden(db), Accounts: acc,
		Watches: repository.NewWatches(db), Sweeps: sw, ArrivalLag: 0, SweepBatch: domain.SweepBatch,
		QueryVectors: repository.NewQueryVectors(db), PhotoVectors: repository.NewPhotoVectors(db),
		Catalog: c, Media: m, LLM: model, Text: text, Image: image, TextVersion: "text-test", ImageVersion: "image-test",
		TextMin: 0.5, PhotoMin: 0.5,
		ModelTimeout: 200 * time.Millisecond, Log: slog.New(slog.DiscardHandler), Now: time.Now,
	}
	for _, o := range opts {
		o(&deps)
	}
	d := service.New(deps)
	return env{db: db, catalog: c, media: m, discovery: d, model: model, text: text, image: image, accounts: acc, sweeps: sw}
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
	reserved := catalogtest.Listing(t, e.db, seller, func(d *catalogdomain.ListingInput, _ *catalogdomain.Refs) { d.Price = 900_000 })
	if err := e.catalog.Reserve(ctx, reserved, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err := e.discovery.Save(ctx, me, reserved); err != nil {
		t.Fatalf("a reserved listing: %v", err)
	}
	p, err := e.discovery.Saved(ctx, me, page.Request{})
	if err != nil || len(p.Items) != 2 || p.Items[1].ListingID != listing || p.Items[1].SavedPrice != 1_500_000 ||
		p.Items[1].Listing.Title == "" || p.Items[1].Listing.CoverThumbURL == "" || p.Items[0].Listing.Status != catalogdomain.StatusReserved ||
		p.Items[1].Listing.CategoryCode != "vehicles.bicycle" || p.Items[1].Listing.Condition != catalogdomain.ConditionUsed ||
		p.Items[1].Listing.Commune == "" {
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
	if l.Status != catalogdomain.StatusHidden || l.Title != "" || l.Price != nil || l.CoverThumbURL != "" ||
		l.CategoryCode != "" || l.Condition != "" || l.Commune != "" {
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
