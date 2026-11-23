// Package service holds module discovery's use cases: a person saves
// listings and reads them back, and searches.
package service

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	"golang.org/x/sync/singleflight"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/discovery/repository"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/llm"
)

// Service is module discovery's contract.
type Service interface {
	// Save keeps someone else's listing on sale or being shipped, with its
	// price; saving it again changes nothing.
	Save(ctx context.Context, account, listing uuid.UUID) error
	Unsave(ctx context.Context, account, listing uuid.UUID) error
	// IsSaved reports whether account saved listing.
	IsSaved(ctx context.Context, account, listing uuid.UUID) (bool, error)
	// Saved lists the account's saves, those of listings still on sale
	// first, each newest first.
	Saved(ctx context.Context, account uuid.UUID, p page.Request) (page.Page[SavedView], error)
	// Search finds active listings; viewer, nil when signed out, sees which
	// they saved.
	Search(ctx context.Context, viewer *uuid.UUID, in SearchInput) (SearchView, error)
	// SellerListings lists what anyone sees of a seller's listings as
	// search items, newest first, with the cursor rules of a page.Request:
	// each measured from near when it names a position. A signed-in
	// viewer's items leave out the listings they hid and are marked saved.
	SellerListings(ctx context.Context, viewer *uuid.UUID, seller uuid.UUID, near catalogdomain.Near, p page.Request) (page.Page[SearchItem], error)
	// ForYou is Home's Dành cho bạn page and its Vì bạn đã xem gần đây rail
	// for viewer, nil when signed out, each card measured from near when it
	// names a position.
	ForYou(ctx context.Context, viewer *uuid.UUID, near catalogdomain.Near) (ForYou, error)
	// History is the account's last domain.MaxHistory text searches,
	// newest first.
	History(ctx context.Context, account uuid.UUID) (HistoryView, error)
	// ForgetSearch deletes one entry of the account's history; an unknown
	// one is fine.
	ForgetSearch(ctx context.Context, account, id uuid.UUID) error
	// ClearHistory empties the account's history.
	ClearHistory(ctx context.Context, account uuid.UUID) error
	// SetAlert sets the price below which account hears of listing, saving
	// it first when it is not saved, with Save's refusals.
	SetAlert(ctx context.Context, account, listing uuid.UUID, below int64) error
	// ClearAlert removes the save's own level; the save stays and tells
	// below its saved price.
	ClearAlert(ctx context.Context, account, listing uuid.UUID) error
	// PriceMoved weighs every save of listing against its current price;
	// the worker calls it on listing.updated and listing.published.
	PriceMoved(ctx context.Context, listing uuid.UUID) error
	// PriceDropFacts is what notification tells of a save's price drop.
	PriceDropFacts(ctx context.Context, id uuid.UUID) (PriceDropFacts, bool, error)
	// Watch follows a text search with its filters from a commune: a new
	// key is a new watch and starts its sweeps; a key the account watches
	// takes the new filters. created reports a new watch.
	Watch(ctx context.Context, account uuid.UUID, in WatchInput) (v WatchView, created bool, err error)
	// Watches lists the account's watches, newest first.
	Watches(ctx context.Context, account uuid.UUID) (WatchList, error)
	// SetWatchAlert mutes or unmutes a watch of the account's.
	SetWatchAlert(ctx context.Context, account, id uuid.UUID, on bool) error
	// Unwatch forgets a watch; an unknown one is fine.
	Unwatch(ctx context.Context, account, id uuid.UUID) error
	// Sweep weighs the listings that arrived since the watch's last sweep;
	// the worker calls it.
	Sweep(ctx context.Context, id uuid.UUID) (Swept, error)
	// ListingArrived records a listing's first publication for the sweeps.
	ListingArrived(ctx context.Context, listing uuid.UUID) error
	// WatchFacts is what notification tells of a watch's new listings.
	WatchFacts(ctx context.Context, id uuid.UUID) (WatchFacts, bool, error)
	// Hide leaves someone else's listing out of account's searches;
	// hiding it again makes it the newest hide.
	Hide(ctx context.Context, account, listing uuid.UUID) error
	// Unhide shows it again; a listing not hidden is fine.
	Unhide(ctx context.Context, account, listing uuid.UUID) error
	// Watchers lists the live watches a listing of seller's matching q
	// would match, muted ones too, oldest first, at most
	// domain.MaxWatchers: by category, condition, area and words, with
	// the price and offers filters left for the caller to weigh.
	Watchers(ctx context.Context, seller uuid.UUID, q WatcherQuery) ([]Watcher, error)
	// Viewed counts viewer's opening of someone else's listing, at most
	// once a UTC day; a deleted account counts nothing.
	Viewed(ctx context.Context, viewer, listing uuid.UUID) error
	// ListingStats reads the views and saves of each listing among ids:
	// all of them, and those of since's UTC day and later, where since is a
	// UTC midnight such as domain.ViewWeek gives. Every ID has an entry.
	ListingStats(ctx context.Context, ids []uuid.UUID, since time.Time) (map[uuid.UUID]ListingStats, error)
	// AccountDeleted deletes what discovery keeps of a deleted account: its
	// saves, history, hides, watches and which listings it opened. The
	// worker calls it on account.deleted.
	AccountDeleted(ctx context.Context, account uuid.UUID) error
}

// Deps is what New builds Discovery from.
type Deps struct {
	Saves          repository.Saves
	Views          repository.Views
	Understandings repository.Understandings
	History        repository.History
	Hidden         repository.Hidden
	Watches        repository.Watches
	// Sweeps starts a new watch's sweeps.
	Sweeps Sweeper
	// ArrivalLag and SweepBatch are domain.ArrivalLag and domain.SweepBatch
	// in production; tests shorten them.
	ArrivalLag time.Duration
	SweepBatch int
	Catalog    catalogsvc.Service
	// Accounts gives the blocks a search leaves out.
	Accounts accountsvc.Service
	LLM      llm.Client
	// Media reads a search photo; Text and Image embed with the search key,
	// under TextVersion and ImageVersion. TextMin and PhotoMin are the
	// floors of the vector lists; LookMin is the photo list's when the
	// query is the look the model wrote, a text.
	Media                      mediasvc.Service
	QueryVectors               repository.QueryVectors
	PhotoVectors               repository.PhotoVectors
	Text, Image                embed.Model
	TextVersion, ImageVersion  string
	TextMin, PhotoMin, LookMin float64
	// ModelTimeout and ModelWait are domain's in production; tests shorten
	// the timeout. A zero ModelWait waits for the model's answer.
	ModelTimeout, ModelWait time.Duration
	Log                     *slog.Logger
	Now                     func() time.Time
}

// Discovery implements Service in this process.
type Discovery struct {
	saves                      repository.Saves
	views                      repository.Views
	understandings             repository.Understandings
	history                    repository.History
	hidden                     repository.Hidden
	watches                    repository.Watches
	sweeps                     Sweeper
	arrivalLag                 time.Duration
	sweepBatch                 int
	catalog                    catalogsvc.Service
	accounts                   accountsvc.Service
	llm                        llm.Client
	media                      mediasvc.Service
	queryVectors               repository.QueryVectors
	photoVectors               repository.PhotoVectors
	text, image                embed.Model
	textVersion, imageVersion  string
	textMin, photoMin, lookMin float64
	modelTimeout, modelWait    time.Duration
	// reads joins the searches that wait for the same query's model call.
	reads singleflight.Group
	log   *slog.Logger
	now   func() time.Time
}

var _ Service = (*Discovery)(nil)

func New(d Deps) *Discovery {
	return &Discovery{
		saves: d.Saves, views: d.Views, understandings: d.Understandings, history: d.History, hidden: d.Hidden, watches: d.Watches,
		sweeps: d.Sweeps, arrivalLag: d.ArrivalLag, sweepBatch: d.SweepBatch, catalog: d.Catalog,
		accounts: d.Accounts, llm: d.LLM,
		media: d.Media, queryVectors: d.QueryVectors, photoVectors: d.PhotoVectors, text: d.Text, image: d.Image,
		textVersion: d.TextVersion, imageVersion: d.ImageVersion, textMin: d.TextMin, photoMin: d.PhotoMin, lookMin: d.LookMin,
		modelTimeout: d.ModelTimeout, modelWait: d.ModelWait, log: d.Log, now: d.Now,
	}
}
