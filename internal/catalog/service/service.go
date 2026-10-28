// Package service holds module catalog's use cases: a seller's listings
// from posting to sale, what anyone may read of them, and the category and
// region lists. Posting and editing run on Restate, in
// listing_post_durable.go and listing_edit_durable.go, and publishing waits
// there for the photos, in listing_publish_durable.go.
package service

import (
	"context"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// Service is module catalog's contract.
//
//nolint:interfacebloat // One method per use case of the listing core; splitting it would split the module.
type Service interface {
	// Post puts a complete listing up for processing. A repeated
	// idempotencyKey returns the listing it posted.
	Post(ctx context.Context, seller uuid.UUID, idempotencyKey string, d domain.Draft) (ListingView, error)
	// Edit replaces a listing's content; a repeated idempotencyKey edits
	// nothing more.
	Edit(ctx context.Context, seller, id uuid.UUID, idempotencyKey string, d domain.Draft) (ListingView, error)
	Hide(ctx context.Context, seller, id uuid.UUID) (ListingView, error)
	Unhide(ctx context.Context, seller, id uuid.UUID) (ListingView, error)
	// MarkSold records a sale made off the app; UnmarkSold undoes it.
	MarkSold(ctx context.Context, seller, id uuid.UUID) (ListingView, error)
	UnmarkSold(ctx context.Context, seller, id uuid.UUID) (ListingView, error)
	Delete(ctx context.Context, seller, id uuid.UUID) error
	// Get reads a listing as viewer, nil when signed out, sees it.
	Get(ctx context.Context, viewer *uuid.UUID, id uuid.UUID) (ListingView, error)
	// ListMine lists one of the seller's panes, newest first.
	ListMine(ctx context.Context, seller uuid.UUID, pane Pane, p page.Request) (page.Page[ListingView], error)
	// SellerListings lists what anyone sees of a seller's listings, newest
	// first.
	SellerListings(ctx context.Context, seller uuid.UUID, p page.Request) (page.Page[ListingView], error)
	// SellerCounts counts a seller's sold listings and those on sale.
	SellerCounts(ctx context.Context, seller uuid.UUID) (SellerCounts, error)
	// Summaries reads listings in any status for other modules; an unknown
	// ID is left out of the map.
	Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Summary, error)
	// Reserve holds an active listing for an order; holding it again for the
	// same order changes nothing. Another status is domain.ErrStateConflict.
	Reserve(ctx context.Context, listingID, orderID uuid.UUID) error
	// Release puts a listing held for the order back on sale; a listing held
	// for another order, or not held, stays as it is.
	Release(ctx context.Context, listingID, orderID uuid.UUID) error
	// SellTo marks a listing held for the order sold; a listing already sold
	// for it stays as it is.
	SellTo(ctx context.Context, listingID, orderID uuid.UUID) error
	// Places reads the communes among codes with their provinces, keyed by
	// code; an unknown code is left out.
	Places(ctx context.Context, codes []string) (map[string]Location, error)
	Categories(ctx context.Context) ([]domain.Category, error)
	// Search runs s and returns its listings as cards, in order.
	Search(ctx context.Context, s domain.Search) ([]SearchCard, error)
	// Counts counts the listings s matches by category or root.
	Counts(ctx context.Context, s domain.Search, by domain.CountField) ([]domain.CategoryCount, error)
	// CommunePoint is a commune's point; false for an unknown code.
	CommunePoint(ctx context.Context, code string) (domain.Point, bool, error)
	// Place resolves a province or commune name to one point; false when no
	// place or more than one has the name.
	Place(ctx context.Context, name string) (domain.Point, bool, error)
	Provinces(ctx context.Context) ([]domain.Province, error)
	Communes(ctx context.Context, provinceCode string) ([]domain.Commune, error)
}

// Deps is what New builds Catalog from.
type Deps struct {
	Listings   repository.Listings
	Categories repository.Categories
	Regions    repository.Regions
	Searches   repository.Searches
	Accounts   accountsvc.Service
	Media      mediasvc.Service
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
}

// Catalog implements Service in this process.
type Catalog struct {
	listings   repository.Listings
	categories repository.Categories
	regions    repository.Regions
	searches   repository.Searches
	accounts   accountsvc.Service
	media      mediasvc.Service
	prefix     string
	now        func() time.Time
	post       restatex.Caller[PostInput, PostResult]
	edit       restatex.Caller[EditInput, EditResult]
}

var _ Service = (*Catalog)(nil)

func New(d Deps) *Catalog {
	return &Catalog{
		listings: d.Listings, categories: d.Categories, regions: d.Regions, searches: d.Searches,
		accounts: d.Accounts, media: d.Media, prefix: d.Prefix, now: d.Now,
		post: restatex.Caller[PostInput, PostResult]{Ingress: d.Ingress, Service: d.Prefix + listingPost, Handler: "Run"},
		edit: restatex.Caller[EditInput, EditResult]{Ingress: d.Ingress, Service: d.Prefix + listingEdit, Handler: "Run"},
	}
}

// Durable returns module catalog's Restate services, for Mount to bind.
func (c *Catalog) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{
		restate.Reflect(ListingPost{
			Prefix: c.prefix, Listings: c.listings, Categories: c.categories, Regions: c.regions,
			Accounts: c.accounts, Media: c.media, Now: c.now,
		}),
		restate.Reflect(ListingEdit{
			Prefix: c.prefix, Listings: c.listings, Categories: c.categories, Regions: c.regions,
			Accounts: c.accounts, Media: c.media, Now: c.now,
		}),
		restate.Reflect(ListingPublish{Prefix: c.prefix, Listings: c.listings, Now: c.now}),
	}
}
