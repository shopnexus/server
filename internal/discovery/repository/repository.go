// Package repository stores module discovery's saved listings and the
// caches of what the model understood of a query and of the vectors a
// search embedded, in Postgres. Each method is its own transaction.
package repository

import (
	"context"
	"encoding/json"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/page"
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
	// List is a query: account's saves after the cursor p.Before(), those
	// whose listing is in a status of last after the others, each newest
	// first, at most p.Fetch().
	List(ctx context.Context, account uuid.UUID, last []catalogdomain.Status, p page.Request) ([]Save, error)
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
	// Counts counts the saves of each listing among ids, all of them and
	// those made at or after since; a listing without a save is absent.
	Counts(ctx context.Context, ids []uuid.UUID, since time.Time) (map[uuid.UUID]Counts, error)
	// Recent lists the listings account saved at or after since, newest
	// save first, at most limit.
	Recent(ctx context.Context, account uuid.UUID, since time.Time, limit int) ([]uuid.UUID, error)
}

// Counts is a listing's count of something, all of it and the part since a
// time.
type Counts struct {
	All   int
	Since int
}

// Views counts the times signed-in people open a listing that is not
// theirs, once per person, listing and day.
type Views interface {
	// Record counts viewer's view of listing on day, a UTC midnight, unless
	// viewer already counted one that day or later, and keeps day as
	// viewer's last view of it. Only an active or reserved listing counts;
	// its seller and a deleted account count nothing. Concurrent records of
	// one viewer and listing count once. It locks no accounts row.
	Record(ctx context.Context, listing, viewer uuid.UUID, day time.Time) error
	// Counts sums the views of each listing among ids, all of them and
	// those of the day from and later; a listing without a view is absent.
	Counts(ctx context.Context, ids []uuid.UUID, from time.Time) (map[uuid.UUID]Counts, error)
	// Clear forgets which listings viewer opened, for its erasure, after
	// the records of viewer in flight commit; the counts stay.
	Clear(ctx context.Context, viewer uuid.UUID) error
	// Recent lists the listings viewer last opened on the day from or
	// later, the latest day first, at most limit. Each mark's Category is
	// empty, as catalog holds the listing's subcategory.
	Recent(ctx context.Context, viewer uuid.UUID, from time.Time, limit int) ([]domain.ViewMark, error)
}

// ModelUses counts the search model's calls for each signed-in person on a
// UTC day.
type ModelUses interface {
	// Take counts one call of account's on now's UTC day and reports true,
	// or reports false when the account made limit calls that day or is
	// deleted.
	Take(ctx context.Context, account uuid.UUID, now time.Time, limit int) (bool, error)
	// Clear deletes the account's count.
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
	ID       uuid.UUID
	Query    string
	Category string // empty when the search understood none
	// CategoryCode is the code of the first category chip, a root or a
	// subcategory, empty with Category.
	CategoryCode string
	PhotoURL     string // empty when the search found nothing
	SearchedAt   time.Time
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

// QueryVectors caches the vectors of texts a search embedded, by the
// model's version and the normalized text. It records no account.
type QueryVectors interface {
	// Get returns the vector of text under model and counts a use at now,
	// at most once per domain.VectorTouchEvery.
	Get(ctx context.Context, model, text string, now time.Time) ([]float32, bool, error)
	// Put stores the vector of text under model, replacing any.
	Put(ctx context.Context, model, text string, v []float32, now time.Time) error
	// Sweep deletes the rows unused for keepUnused that no search read
	// again, those unused for keepUsed, and then the least used rows past
	// limit, and returns how many it deleted.
	Sweep(ctx context.Context, now time.Time, keepUnused, keepUsed time.Duration, limit int) (int64, error)
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

// WatchCandidate is a watch whose category and conditions admit a
// listing, with what is left to weigh: its words, its price and offers
// filters, and its area (a zero Point without one).
type WatchCandidate struct {
	Query              string
	Phrases            []string
	PriceMin, PriceMax int64
	OffersOnly         bool
	RadiusKm           int
	Point              catalogdomain.Point
}

// CandidateFilter is a listing's category, its root and its condition, and
// the accounts whose watches stay out.
type CandidateFilter struct {
	CategoryCode, CategoryRoot, Condition string
	Exclude                               []uuid.UUID
	Limit                                 int
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
	// Candidates lists the watches of active accounts not in f.Exclude
	// whose category is none, f's code or f's root and whose conditions are
	// none or hold f's, muted ones too, oldest first, at most f.Limit.
	// f.Exclude must not be nil.
	Candidates(ctx context.Context, f CandidateFilter) ([]WatchCandidate, error)
}
