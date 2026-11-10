// Package repository stores module catalog's listings, categories and
// regions in Postgres. Each method is its own transaction.
package repository

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
)

var (
	ErrNotFound = errors.New("catalog: not found")
	ErrConflict = errors.New("catalog: changed meanwhile")
)

// Listings persists listings.
type Listings interface {
	// Insert adds l unless its seller already has domain.MaxOnSale listings
	// in a domain.OnSale status, which is domain.ErrCapReached.
	// Inserting the same ID again does nothing. Concurrent inserts and
	// saves for one seller run one at a time, so the cap holds.
	Insert(ctx context.Context, l domain.Listing) error
	// Discard deletes the row of a listing the post flow inserted and could
	// not finish; only that compensation calls it. An absent row is fine.
	Discard(ctx context.Context, id uuid.UUID) error
	// CountByStatus is a query: how many listings seller has in each
	// status; a status without one is left out.
	CountByStatus(ctx context.Context, seller uuid.UUID) (map[domain.Status]int, error)
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Listing, error)
	// GetMany returns the listings among ids in any status and order; an
	// unknown ID is left out.
	GetMany(ctx context.Context, ids []uuid.UUID) ([]domain.Listing, error)
	// Save writes l and evs when its row still has l.WriteID() or already
	// has writeID, then sets its write ID. Otherwise ErrConflict and nothing
	// is written. A listing coming back on sale, from sold, is
	// domain.ErrCapReached when its seller is at the cap.
	Save(ctx context.Context, l *domain.Listing, writeID uuid.UUID, evs ...events.Event) error
	// ListBySeller is a query: seller's listings in statuses with IDs below
	// p.Before(), newest first, at most p.Fetch().
	ListBySeller(ctx context.Context, seller uuid.UUID, statuses []domain.Status, p page.Request) ([]domain.Listing, error)
}

// Categories reads the category tree.
type Categories interface {
	// Tree returns every category: the roots in order, then each root's
	// subcategories in order.
	Tree(ctx context.Context) ([]domain.Category, error)
	// IsLeaf reports whether key is a subcategory.
	IsLeaf(ctx context.Context, key string) (bool, error)
}

// Regions reads the administrative units.
type Regions interface {
	Provinces(ctx context.Context) ([]domain.Province, error)
	// Communes returns a province's communes by display name.
	Communes(ctx context.Context, provinceCode string) ([]domain.Commune, error)
	// Places returns the communes among codes with their provinces' display
	// names, in any order; an unknown code is left out.
	Places(ctx context.Context, codes []string) ([]Place, error)
	// FindPlaces returns, for each of names, the points of the provinces
	// and communes whose display name, with or without its type ("Thành
	// phố", "Tỉnh", "Phường", "Xã", "Đặc khu"), is that name, ignoring case.
	// A province's point is the mean of its communes'. A name that matches
	// nothing is left out.
	FindPlaces(ctx context.Context, names []string) (map[string][]domain.Point, error)
	// Nearest returns the commune whose point is closest to p.
	Nearest(ctx context.Context, p domain.Point) (Place, error)
}

// Searches runs searches over the listings' ParadeDB index.
type Searches interface {
	// Search returns a page of the listings s finds, in its order.
	Search(ctx context.Context, s domain.Search) ([]domain.Hit, error)
	// Counts counts the listings s matches, boosts aside, by category or
	// root, largest first.
	Counts(ctx context.Context, s domain.Search, by domain.CountField) ([]domain.CategoryCount, error)
}

// Vectors persists the search vectors of listings: one text vector and one
// vector per processed photo, each under a model version.
type Vectors interface {
	// State is what listing has under the versions: its text hash, nil for
	// none, and the photo keys that have a vector.
	State(ctx context.Context, listing uuid.UUID, textVersion, photoVersion string) (VectorState, error)
	// PutText writes listing's text vector, replacing any.
	PutText(ctx context.Context, listing uuid.UUID, version string, hash []byte, v []float32, at time.Time) error
	// PutPhoto writes the vector of one processed photo, replacing any.
	PutPhoto(ctx context.Context, listing uuid.UUID, key, version string, v []float32, at time.Time) error
	// DropPhotosExcept deletes listing's photo vectors whose key is not in
	// keys, of any version.
	DropPhotosExcept(ctx context.Context, listing uuid.UUID, keys []string) error
	// Source reads a similar-listings search's query vectors: listing's text
	// vector and the normalized mean of its photo vectors; nil for what it
	// does not have.
	Source(ctx context.Context, listing uuid.UUID, textVersion, photoVersion string) (text, photo []float32, err error)
	// Embeddable lists the listings that should have vectors: every one not
	// deleted, processing or rejected.
	Embeddable(ctx context.Context) ([]uuid.UUID, error)
	// PriceSample returns the prices of the listings like f's vectors, most
	// similar first, at most f.Limit; nil when f has no vector.
	PriceSample(ctx context.Context, f PriceFilter) ([]domain.PriceSample, error)
}

// VectorState is what vectors a listing has.
type VectorState struct {
	TextHash []byte
	Photos   []string
}

// PriceFilter is what a price sample draws from: listings of CategoryCode
// in Condition, not Exclude's, sold since SoldSince or active or reserved
// and published since OnSaleSince. A listing joins when its text vector, or
// its closest photo vector, under the version given is at cosine TextMin or
// PhotoMin or more; its similarity is the better of the two. A nil Text or
// Photo leaves that side out.
type PriceFilter struct {
	Exclude                   uuid.UUID
	CategoryCode              string
	Condition                 domain.Condition
	Text, Photo               []float32
	TextVersion, PhotoVersion string
	TextMin, PhotoMin         float64
	SoldSince, OnSaleSince    time.Time
	Limit                     int
}

// Place is a commune with its province's display name.
type Place struct {
	domain.Commune

	Province string
}
