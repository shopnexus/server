// Package repository stores module boost's purchases.
package repository

import (
	"context"
	"errors"
	"uuid"

	"github.com/shopnexus/server/internal/boost/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
)

// ErrConflict is a save that lost to another write; the caller reads again.
var ErrConflict = errors.New("boost: changed meanwhile")

// Store stores boosts. Inserts for one listing take its lock, so the
// listing's live boost is read and replaced one purchase at a time.
type Store interface {
	// Insert stores b, awaiting payment, and returns it with false. It
	// returns instead, with true: the boost the seller's key made already,
	// or the listing's boost of the same package awaiting payment. A
	// running one is domain.ErrActive with its ID; one of another package
	// awaiting payment is cancelled and b stored. A deleted seller is
	// accountdomain.ErrSessionRevoked.
	Insert(ctx context.Context, b domain.Boost) (domain.Boost, bool, error)
	// ByKey reads the boost seller's key made; domain.ErrNotFound for none.
	ByKey(ctx context.Context, seller, key uuid.UUID) (domain.Boost, error)
	// Get reads a boost; domain.ErrNotFound for none.
	Get(ctx context.Context, id uuid.UUID) (domain.Boost, error)
	// ByPayment reads the boost a payment pays for; domain.ErrNotFound for
	// none.
	ByPayment(ctx context.Context, payment uuid.UUID) (domain.Boost, error)
	// Live reads the boost awaiting payment or running on each of
	// listings, keyed by listing.
	Live(ctx context.Context, listings []uuid.UUID) (map[uuid.UUID]domain.Boost, error)
	// OfListing reads the listing's boosts newest first, below p's cursor,
	// at most p.Fetch().
	OfListing(ctx context.Context, listing uuid.UUID, p page.Request) ([]domain.Boost, error)
	// Save writes b under writeID with evs when its write ID is still the
	// stored one; ErrConflict otherwise, and nothing is written.
	Save(ctx context.Context, b *domain.Boost, writeID uuid.UUID, evs ...events.Event) error
}
