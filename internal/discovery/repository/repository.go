// Package repository stores module discovery's saved listings and the
// cache of what the model understood of a query, in Postgres. Each method
// is its own transaction.
package repository

import (
	"context"
	"encoding/json"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
)

// Saves persists the listings people save.
type Saves interface {
	// Save records that account saved listing at price, as id; saving it
	// again changes nothing. A save beyond domain.MaxSaved is
	// domain.ErrLimit; concurrent saves of one account count one at a time.
	Save(ctx context.Context, id, account, listing uuid.UUID, price int64, at time.Time) error
	// Remove forgets a save; an absent one is fine.
	Remove(ctx context.Context, account, listing uuid.UUID) error
	// Has reports whether account saved listing.
	Has(ctx context.Context, account, listing uuid.UUID) (bool, error)
	// SavedAmong returns which of ids account saved.
	SavedAmong(ctx context.Context, account uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error)
	// List is a query: account's saves with IDs below p.Before(), newest
	// first, at most p.Fetch().
	List(ctx context.Context, account uuid.UUID, p page.Request) ([]Save, error)
}

// Save is one saved listing with the price it had when saved.
type Save struct {
	ID        uuid.UUID
	ListingID uuid.UUID
	Price     int64
	At        time.Time
}

// Understandings caches the model's answers by normalized query and prompt
// version. It records no account.
type Understandings interface {
	// Get returns the answer stored for key and version at or after since.
	Get(ctx context.Context, key string, version int, since time.Time) (json.RawMessage, bool, error)
	// Put stores answer for key and version at at, replacing any, and
	// deletes the answers stored before expired, in one transaction.
	Put(ctx context.Context, key string, version int, answer json.RawMessage, at, expired time.Time) error
}
