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
	// Save writes d, its latest order, its pending moves and evs when its
	// row still has d.WriteID(), then sets its write ID. A row that already
	// has writeID was written by an earlier try of this save: Save writes
	// nothing more and returns nil. Otherwise ErrConflict and nothing is
	// written. A requested order beyond the buyer's domain.MaxOpenOrders is
	// domain.ErrCodLimit; concurrent saves for one buyer count one at a
	// time.
	Save(ctx context.Context, d *domain.Deal, writeID uuid.UUID, evs ...events.Event) error
	// OfListing returns every deal on a listing, oldest first.
	OfListing(ctx context.Context, listingID uuid.UUID) ([]domain.Deal, error)
	// ListByParty is a query: account's deals on side as (buyer or
	// seller) with IDs below p.Before(), newest first, at most p.Fetch().
	ListByParty(ctx context.Context, account uuid.UUID, as domain.Party, p page.Request) ([]domain.Deal, error)
	// Between returns the deals of a and b on either side, newest first.
	Between(ctx context.Context, a, b uuid.UUID) ([]domain.Deal, error)
	// ListOrders is a query: account's orders on side as with IDs below
	// p.Before(), newest first, at most p.Fetch().
	ListOrders(ctx context.Context, account uuid.UUID, as domain.Party, p page.Request) ([]OrderRow, error)
	// Moves returns each deal's moves, oldest first; a deal without moves
	// is absent.
	Moves(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.Move, error)
}

// OrderRow is an order with the deal it belongs to.
type OrderRow struct {
	DealID    uuid.UUID
	ListingID uuid.UUID
	BuyerID   uuid.UUID
	SellerID  uuid.UUID
	Order     domain.Order
}
