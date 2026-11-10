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
	// Get reads one save by its ID.
	Get(ctx context.Context, id uuid.UUID) (Save, bool, error)
	// SetAlert sets the level of account's save of listing, nil to clear
	// it, and forgets the price it last told; found is false without the
	// save.
	SetAlert(ctx context.Context, account, listing uuid.UUID, below *int64) (found bool, err error)
	// PriceMoved applies domain.PriceStep to every save of listing at price,
	// and writes events.SavedPriceDropped for each save it tells, in one
	// transaction. It returns how many it told.
	PriceMoved(ctx context.Context, listing uuid.UUID, price int64, at time.Time) (int, error)
}

// Settings keeps a person's discovery settings: the switch that alerts on
// a drop below the saved price of every save without its own level.
type Settings interface {
	// PriceCutAlerts is the switch; an account that never set it reads on.
	PriceCutAlerts(ctx context.Context, account uuid.UUID) (bool, error)
	SetPriceCutAlerts(ctx context.Context, account uuid.UUID, on bool) error
	Clear(ctx context.Context, account uuid.UUID) error
}

// Hidden keeps the listings a person hid from their searches.
type Hidden interface {
	// Hide hides listing from account's searches as the newest hide, again
	// too, and drops the hides past domain.MaxHidden, in one transaction.
	Hide(ctx context.Context, account, listing uuid.UUID) error
	// Unhide shows it again; a listing not hidden is fine.
	Unhide(ctx context.Context, account, listing uuid.UUID) error
	// List is the hidden listings, newest hide first.
	List(ctx context.Context, account uuid.UUID) ([]uuid.UUID, error)
	// Clear deletes every hide of the account.
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

// Save is one saved listing with the price it had when saved and its own
// alert level, nil for none.
type Save struct {
	ID         uuid.UUID
	ListingID  uuid.UUID
	Price      int64
	At         time.Time
	AlertBelow *int64
	Account    uuid.UUID
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

// Watch is a person's followed search: the query, the phrases the model
// read from it, the filters and a commune. Zero values are no filter.
type Watch struct {
	ID          uuid.UUID
	Account     uuid.UUID
	Query       string
	Phrases     []string
	Category    string
	PriceMin    int64
	PriceMax    int64
	Conditions  []string
	OffersOnly  bool
	Commune     string
	RadiusKm    int
	Alert       bool
	NewCount    int
	LastArrival int64
	CreatedAt   time.Time
}

// Arrival is a listing's first publication, in the order it committed.
type Arrival struct {
	Seq       int64
	ListingID uuid.UUID
}

// Watches persists the search watches and the arrivals they sweep.
type Watches interface {
	// Put stores w under domain.HistoryKey of its query. A new key inserts
	// w with last_arrival at the newest arrival; past domain.MaxWatches it
	// is domain.ErrWatchLimit, counted one at a time per account. A key the
	// account watches takes w's query, phrases and filters, keeps its ID
	// and alert, and sets new_count to 0. It returns the stored watch and
	// whether it is new.
	Put(ctx context.Context, w Watch) (Watch, bool, error)
	Get(ctx context.Context, id uuid.UUID) (Watch, bool, error)
	// List is the account's watches, newest first.
	List(ctx context.Context, account uuid.UUID) ([]Watch, error)
	// ByKey is the ID of account's watch of key.
	ByKey(ctx context.Context, account uuid.UUID, key string) (uuid.UUID, bool, error)
	// SetAlert mutes or unmutes account's watch; found is false for
	// another account's or an unknown one.
	SetAlert(ctx context.Context, account, id uuid.UUID, on bool) (found bool, err error)
	// Seen sets the watch's new_count to 0.
	Seen(ctx context.Context, account, id uuid.UUID) error
	// Delete forgets account's watch; an unknown one is fine.
	Delete(ctx context.Context, account, id uuid.UUID) error
	// Clear deletes every watch of the account.
	Clear(ctx context.Context, account uuid.UUID) error
	// Arrive records listing's first publication, once, and deletes up to
	// 1,000 arrivals older than domain.KeepArrivals.
	Arrive(ctx context.Context, listing uuid.UUID) error
	// Arrivals lists the arrivals after seq after, oldest first, at most
	// limit, and stops at the first one younger than lag: no sweep moves
	// past an arrival whose transaction may still be committing.
	Arrivals(ctx context.Context, after int64, lag time.Duration, limit int) ([]Arrival, error)
	// Swept moves the watch's last_arrival from prev to through and adds n
	// to new_count, at most domain.MaxNewCount; with n > 0 and the alert on
	// it writes events.SearchWatchMatched, in one transaction. A watch no
	// longer at prev moves nothing, and moved is false.
	Swept(ctx context.Context, id uuid.UUID, prev, through int64, n int, at time.Time) (moved bool, err error)
}
