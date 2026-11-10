// Package repository stores module discovery's saved listings and the
// caches of what the model understood of a query and of the vectors a
// search embedded, in Postgres. Each method is its own transaction.
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
	// SavedAmong returns which of ids account saved.
	SavedAmong(ctx context.Context, account uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error)
	// List is a query: account's saves with IDs below p.Before(), newest
	// first, at most p.Fetch().
	List(ctx context.Context, account uuid.UUID, p page.Request) ([]Save, error)
	// Clear deletes every save of the account.
	Clear(ctx context.Context, account uuid.UUID) error
}

// History keeps a person's last domain.MaxHistory text searches.
type History interface {
	// Remember puts e at the top: the same words (domain.HistoryKey) again
	// take a new ID and e's fields, and the entries past
	// domain.MaxHistory go, in one transaction.
	Remember(ctx context.Context, account uuid.UUID, e HistoryEntry) error
	// List is the history, newest first.
	List(ctx context.Context, account uuid.UUID) ([]HistoryEntry, error)
	// Forget deletes one entry of the account's; an unknown one is fine.
	Forget(ctx context.Context, account, id uuid.UUID) error
	// Clear deletes the account's history.
	Clear(ctx context.Context, account uuid.UUID) error
}

// HistoryEntry is one search of a person's history.
type HistoryEntry struct {
	ID         uuid.UUID
	Query      string
	Category   string // empty when the search understood none
	PhotoURL   string // empty when the search found nothing
	SearchedAt time.Time
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

// QueryVectors caches the vectors of texts a search embedded. It records no account.
type QueryVectors interface {
	Get(ctx context.Context, key string, promptVersion int, model string, since time.Time) ([]float32, bool, error)
	Put(ctx context.Context, key string, promptVersion int, model string, v []float32, at, expired time.Time) error
}

// PhotoVectors caches the vector of a search photo until its upload expires.
type PhotoVectors interface {
	Get(ctx context.Context, upload uuid.UUID, model string, now time.Time) ([]float32, bool, error)
	Put(ctx context.Context, upload uuid.UUID, model string, v []float32, expires, now time.Time) error
}
