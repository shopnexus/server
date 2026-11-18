// Package catalogtest builds module catalog and puts listings on sale for
// the tests of modules that need something to sell.
package catalogtest

import (
	"context"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/catalog/service"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/provider/embed/embedtest"
)

// Service is module catalog on db with the account and media services it
// calls, reaching its flows through in under prefix. Each change edits the
// dependencies before the service is built, to wrap one with a fault.
func Service(
	db *pgxpool.Pool, accounts accountsvc.Service, media mediasvc.Service, in *ingress.Client, prefix string,
	change ...func(*service.Deps),
) *service.Catalog {
	deps := service.Deps{
		Listings: repository.NewListings(db), Drafts: repository.NewDrafts(db), Categories: repository.NewCategories(db), Regions: repository.NewRegions(db),
		Searches: repository.NewSearches(db), Accounts: accounts, Media: media,
		Ingress: in, Prefix: prefix, Now: time.Now,
		Vectors: repository.NewVectors(db), Text: embedtest.New(domain.TextDims), Image: embedtest.New(domain.PhotoDims),
		TextVersion: "text-test", ImageVersion: "image-test", EmbedSlots: 2, Log: slog.New(slog.DiscardHandler),
	}
	for _, c := range change {
		c(&deps)
	}
	return service.New(deps)
}

// Listing puts a listing of seller's on sale and returns its ID: "Xe đạp
// Asama", a used bicycle at 1,500,000 đ that takes offers, in commune
// 00004, with one processed photo. Each change edits the listing input or the
// references before the listing is built.
func Listing(tb testing.TB, db *pgxpool.Pool, seller uuid.UUID, change ...func(*domain.ListingInput, *domain.Refs)) uuid.UUID {
	tb.Helper()
	photo := uuid.NewV7()
	now := time.Now()
	d := domain.ListingInput{
		Title: "Xe đạp Asama", Price: 1_500_000, CategoryCode: "vehicles.bicycle", Condition: domain.ConditionUsed,
		AllowOffers: true, CommuneCode: "00004", Photos: []uuid.UUID{photo},
	}
	refs := domain.Refs{CategoryIsLeaf: true, CommuneExists: true}
	for _, c := range change {
		c(&d, &refs)
	}
	l, err := domain.NewListing(uuid.NewV7(), seller, d, refs, now)
	if err != nil {
		tb.Fatal(err)
	}
	l.Published(map[uuid.UUID]domain.PhotoKeys{photo: {Photo: "media/a.jpg", Thumb: "media/a_thumb.jpg"}}, now)
	if err = repository.NewListings(db).Insert(context.Background(), l); err != nil {
		tb.Fatal(err)
	}
	return l.ID()
}
