// Package service holds module catalog's use cases: a seller's listings
// from posting to sale, what anyone may read of them, and the category and
// region lists. Posting and editing run on Restate, in
// listing_post_durable.go and listing_edit_durable.go, and publishing waits
// there for the photos, in listing_publish_durable.go.
package service

import (
	"context"
	"log/slog"
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
	"github.com/shopnexus/server2/internal/provider/embed"
)

// Service is module catalog's contract.
type Service interface {
	// Post puts a complete listing up for processing. A repeated
	// idempotencyKey returns the listing it posted.
	Post(ctx context.Context, seller uuid.UUID, idempotencyKey string, d domain.ListingInput) (ListingView, error)
	// Edit replaces a listing's content; a repeated idempotencyKey edits
	// nothing more.
	Edit(ctx context.Context, seller, id uuid.UUID, idempotencyKey string, d domain.ListingInput) (ListingView, error)
	Hide(ctx context.Context, seller, id uuid.UUID) (ListingView, error)
	Unhide(ctx context.Context, seller, id uuid.UUID) (ListingView, error)
	// MarkSold records a sale made off the app; UnmarkSold undoes it.
	MarkSold(ctx context.Context, seller, id uuid.UUID) (ListingView, error)
	UnmarkSold(ctx context.Context, seller, id uuid.UUID) (ListingView, error)
	Delete(ctx context.Context, seller, id uuid.UUID) error
	// PutDraft writes the caller's draft id, the last write winning, and
	// returns it as stored and whether it was created.
	PutDraft(ctx context.Context, account, id uuid.UUID, c domain.DraftContent) (DraftView, bool, error)
	// ListDrafts lists the caller's drafts in full, last written first.
	ListDrafts(ctx context.Context, account uuid.UUID) ([]DraftView, error)
	// DeleteDraft deletes one of the caller's drafts; none changes nothing.
	DeleteDraft(ctx context.Context, account, id uuid.UUID) error
	// Get reads a listing as viewer, nil when signed out, sees it.
	Get(ctx context.Context, viewer *uuid.UUID, id uuid.UUID) (ListingView, error)
	// ListMine lists one of the seller's panes, newest first.
	ListMine(ctx context.Context, seller uuid.UUID, pane Pane, p page.Request) (page.Page[ListingView], error)
	// SellerListings lists what anyone sees of a seller's listings, newest
	// first, with their distances from near when it names a position.
	SellerListings(ctx context.Context, seller uuid.UUID, near domain.Near, p page.Request) (SellerListingPage, error)
	// Distance is how far listing id is from near in whole kilometres, nil
	// when near names no position.
	Distance(ctx context.Context, near domain.Near, id uuid.UUID) (*int, error)
	// Origin is the point near names, nil for none; a bad point or an
	// unknown commune is domain.ErrInvalidPosition.
	Origin(ctx context.Context, near domain.Near) (*domain.Point, error)
	// SellerCounts counts a seller's sold listings, the public ones and
	// those under domain.MaxOnSale.
	SellerCounts(ctx context.Context, seller uuid.UUID) (SellerCounts, error)
	// Summaries reads listings in any status for other modules; an unknown
	// ID is left out of the map.
	Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Summary, error)
	// Summary reads one listing in any status for other modules; ok is false
	// for an unknown ID.
	Summary(ctx context.Context, id uuid.UUID) (s Summary, ok bool, err error)
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
	// Locate is the commune whose point is closest to p, with its province;
	// domain.ErrNoCommuneNear when that point is farther than
	// domain.MaxGPSKm.
	Locate(ctx context.Context, p domain.Point) (Location, error)
	Categories(ctx context.Context) ([]domain.Category, error)
	// Search runs s and returns its listings as cards, in order.
	Search(ctx context.Context, s domain.Search) ([]SearchCard, error)
	// Words splits text into the words the listings index would find in
	// it, for matching a listing that is not posted yet.
	Words(ctx context.Context, text string) ([]string, error)
	// Counts counts the listings s matches by category or root.
	Counts(ctx context.Context, s domain.Search) ([]domain.CategoryCount, error)
	// EmbeddingText is the text a listing posted from d would be embedded
	// from, with its category's display name.
	EmbeddingText(ctx context.Context, d domain.ListingInput) (string, error)
	// PriceSample returns the listings like q's vectors with their prices,
	// days to sell and points, most similar first, at most domain.PriceSampleSize; nil without a vector.
	PriceSample(ctx context.Context, q PriceQuery) ([]PriceSample, error)
	// MarketPrice compares a listing's price with a price sample drawn with
	// its own vectors at the cosine floors given, its seller's listings left
	// out; false for an unknown listing.
	MarketPrice(ctx context.Context, listing uuid.UUID, textMin, photoMin float64) (MarketPrice, bool, error)
	// FindPlaces resolves province and commune names to points, keyed by
	// the name as given; a name that no place or more than one has is left
	// out.
	FindPlaces(ctx context.Context, names []string) (map[string]domain.Point, error)
	Provinces(ctx context.Context) ([]domain.Province, error)
	Communes(ctx context.Context, provinceCode string) ([]domain.Commune, error)
	// SellerLeft deletes a deleted seller's drafts and every listing of
	// theirs that is not sold or deleted, each with listing.deleted; the
	// worker calls it on account.deleted, and a repeated call finds nothing
	// left.
	SellerLeft(ctx context.Context, seller uuid.UUID) error
}

// Deps is what New builds Catalog from.
type Deps struct {
	Listings   repository.Listings
	Drafts     repository.Drafts
	Categories repository.Categories
	Regions    repository.Regions
	Searches   repository.Searches
	Accounts   accountsvc.Service
	Media      mediasvc.Service
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
	Vectors repository.Vectors
	// Text and Image embed with the index key; TextVersion and ImageVersion
	// name the models behind them and go into every vector row.
	Text, Image               embed.Model
	TextVersion, ImageVersion string
	// EmbedSlots is how many embeddings ListingEmbed runs at once in this
	// process.
	EmbedSlots int
	Log        *slog.Logger
}

// Catalog implements Service in this process.
type Catalog struct {
	listings                  repository.Listings
	drafts                    repository.Drafts
	categories                repository.Categories
	regions                   repository.Regions
	searches                  repository.Searches
	accounts                  accountsvc.Service
	media                     mediasvc.Service
	prefix                    string
	now                       func() time.Time
	post                      restatex.Caller[PostInput, PostResult]
	edit                      restatex.Caller[EditInput, EditResult]
	vectors                   repository.Vectors
	text, image               embed.Model
	textVersion, imageVersion string
	embedSlots                chan struct{}
	log                       *slog.Logger
}

var _ Service = (*Catalog)(nil)

func New(d Deps) *Catalog {
	return &Catalog{
		listings: d.Listings, drafts: d.Drafts, categories: d.Categories, regions: d.Regions, searches: d.Searches,
		accounts: d.Accounts, media: d.Media, prefix: d.Prefix, now: d.Now,
		vectors: d.Vectors, text: d.Text, image: d.Image, textVersion: d.TextVersion, imageVersion: d.ImageVersion,
		embedSlots: make(chan struct{}, d.EmbedSlots), log: d.Log,
		post: restatex.Caller[PostInput, PostResult]{Ingress: d.Ingress, Service: d.Prefix + listingPost, Handler: "Run"},
		edit: restatex.Caller[EditInput, EditResult]{Ingress: d.Ingress, Service: d.Prefix + listingEdit, Handler: "Run"},
	}
}

// Durable returns module catalog's Restate services, for Mount to bind.
func (c *Catalog) Durable() []restate.ServiceDefinition {
	deps := flowDeps{
		Prefix: c.prefix, Listings: c.listings, Categories: c.categories, Regions: c.regions,
		Accounts: c.accounts, Media: c.media, Now: c.now,
	}
	return []restate.ServiceDefinition{
		restate.Reflect(ListingPost{deps}),
		restate.Reflect(ListingEdit{deps}),
		restate.Reflect(ListingPublish{Prefix: c.prefix, Listings: c.listings, Now: c.now}),
		restate.Reflect(ListingEmbed{
			Prefix: c.prefix, Listings: c.listings, Categories: c.categories, Vectors: c.vectors, Media: c.media,
			Text: c.text, Image: c.image, TextVersion: c.textVersion, ImageVersion: c.imageVersion,
			Slots: c.embedSlots, Log: c.log, Now: c.now,
		}),
	}
}
