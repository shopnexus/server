// Package repository stores module discovery's saved listings in Postgres.
// Each method is its own transaction.
package repository

import (
	"context"
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
