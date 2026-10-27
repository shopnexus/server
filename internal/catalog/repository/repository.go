// Package repository stores module catalog's listings, categories and
// regions in Postgres. Each method is its own transaction.
package repository

import (
	"context"
	"errors"
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
	// on sale or hidden (domain.OnSale), which is domain.ErrCapReached.
	// Inserting the same ID again does nothing. Concurrent inserts and
	// saves for one seller run one at a time, so the cap holds.
	Insert(ctx context.Context, l domain.Listing) error
	// Discard deletes the row of a listing the post flow inserted and could
	// not finish; only that compensation calls it. An absent row is fine.
	Discard(ctx context.Context, id uuid.UUID) error
	// CountOnSale counts seller's listings on sale or hidden.
	CountOnSale(ctx context.Context, seller uuid.UUID) (int, error)
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
	CommuneExists(ctx context.Context, code string) (bool, error)
	// Places returns the communes among codes with their provinces' display
	// names, in any order; an unknown code is left out.
	Places(ctx context.Context, codes []string) ([]Place, error)
}

// Place is a commune with its province's display name.
type Place struct {
	domain.Commune

	Province string
}
