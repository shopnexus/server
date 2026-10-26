// Package repository stores module deal's deals and their moves in
// Postgres. Each method is its own transaction.
package repository

import (
	"context"
	"errors"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
)

var (
	ErrNotFound = errors.New("deal: not found")
	ErrConflict = errors.New("deal: changed meanwhile")
)

// Deals persists deals and their moves.
type Deals interface {
	// Insert adds d and reports true, or false when the buyer already has
	// a deal on the listing; then nothing is written.
	Insert(ctx context.Context, d domain.Deal) (bool, error)
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Deal, error)
	// Find returns the buyer's deal on a listing, or ErrNotFound.
	Find(ctx context.Context, listingID, buyer uuid.UUID) (domain.Deal, error)
	// Save writes d, its pending moves and evs when its row still has
	// d.WriteID() or already has writeID, then sets its write ID.
	// Otherwise ErrConflict and nothing is written.
	Save(ctx context.Context, d *domain.Deal, writeID uuid.UUID, evs ...events.Event) error
	// OfListing returns every deal on a listing, oldest first.
	OfListing(ctx context.Context, listingID uuid.UUID) ([]domain.Deal, error)
	// ListByParty is a query: account's deals on side as (buyer or
	// seller) with IDs below p.Before(), newest first, at most p.Fetch().
	ListByParty(ctx context.Context, account uuid.UUID, as domain.Party, p page.Request) ([]domain.Deal, error)
	// Moves returns each deal's moves, oldest first; a deal without moves
	// is absent.
	Moves(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.Move, error)
}
