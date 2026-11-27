// Package domain holds module boost's entity: one purchase of a package
// that moves a listing to the top of the newest order once a day.
package domain

import (
	"encoding/json"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// Every is the time between two moves up of a running package.
const Every = 24 * time.Hour

var (
	ErrNotFound       = apperr.New(apperr.KindNotFound, "boost_not_found", "no such boost")
	ErrUnknownPackage = apperr.New(apperr.KindInvalid, "unknown_boost_package", "no boost package has that id")
	// ErrNotBoostable is a listing not on sale: only one buyers can find is
	// boosted.
	ErrNotBoostable = apperr.New(apperr.KindConflict, "listing_not_boostable", "the listing is not on sale")
	// ErrActive is a listing a package runs on; one at a time (BR-LST-05).
	ErrActive = apperr.New(apperr.KindConflict, "boost_active", "a boost package runs on the listing")
	// ErrPending is a purchase of another package while one awaits payment
	// on the listing; that one is paid or lapses first (BR-LST-05).
	ErrPending = apperr.New(apperr.KindConflict, "boost_pending", "another boost package awaits payment on the listing")
)

// Package is a package sellers buy: Days of moves up for Price đồng.
type Package struct {
	ID    string
	Days  int
	Price int64
}

// PackageOf names a package by its days, such as 3d.
func PackageOf(days int, price int64) Package {
	return Package{ID: fmt.Sprintf("%dd", days), Days: days, Price: price}
}

// Status is where a boost is.
type Status string

const (
	StatusAwaiting Status = "awaiting_payment"
	StatusActive   Status = "active"
	StatusEnded    Status = "ended"
	StatusFailed   Status = "failed"
	StatusExpired  Status = "expired"
)

// Boost is one purchase. Its fields are its stored state.
type Boost struct{ s State }

// NewBoost is a purchase of pkg for the seller's listing, awaiting its
// payment. The first write ID is the id.
func NewBoost(id, listing, seller, key uuid.UUID, pkg Package, payment uuid.UUID, now time.Time) Boost {
	return Boost{s: State{
		ID: id, ListingID: listing, SellerID: seller, RequestKey: key, Package: pkg.ID, Days: pkg.Days, Price: pkg.Price,
		Status: StatusAwaiting, PaymentID: payment, CreatedAt: now, WriteID: id,
	}}
}

// Start runs a paid boost from paid, when its payment landed, and reports
// whether it did. bumped is the first move up, nil when the listing was
// not on sale then. A boost whose payment failed or expired starts too:
// paid money always buys the package (BR-LST-07). Days count from paid.
func (b *Boost) Start(paid time.Time, bumped *time.Time) bool {
	switch b.s.Status {
	case StatusAwaiting, StatusFailed, StatusExpired:
	case StatusActive, StatusEnded:
		return false
	default:
		return false
	}
	ends := paid.Add(time.Duration(b.s.Days) * Every)
	next := earlier(paid.Add(Every), ends)
	b.s.Status, b.s.StartedAt, b.s.EndsAt, b.s.NextTickAt, b.s.EndedAt = StatusActive, &paid, &ends, &next, nil
	if bumped != nil {
		b.s.LastBumpAt, b.s.Bumps = bumped, 1
	}
	return true
}

// Close ends a boost awaiting payment whose payment failed or expired,
// with that status, and reports whether it did.
func (b *Boost) Close(status Status, at time.Time) bool {
	if b.s.Status != StatusAwaiting || (status != StatusFailed && status != StatusExpired) {
		return false
	}
	b.s.Status, b.s.EndedAt = status, &at
	return true
}

// Due reports whether the tick of ref is the running boost's next one; a
// tick of a boost that moved on does nothing.
func (b *Boost) Due(ref time.Time) bool {
	return b.s.Status == StatusActive && b.s.NextTickAt != nil && b.s.NextTickAt.Equal(ref)
}

// Last reports whether the tick of ref ends the package.
func (b *Boost) Last(ref time.Time) bool { return b.s.EndsAt != nil && !ref.Before(*b.s.EndsAt) }

// End closes a running package whose days have passed.
func (b *Boost) End(at time.Time) {
	b.s.Status, b.s.EndedAt, b.s.NextTickAt = StatusEnded, &at, nil
}

// Ticked records the tick of ref at at, a move up when bumped (the listing
// was on sale then), and sets the next tick, the end's at the latest.
func (b *Boost) Ticked(ref, at time.Time, bumped bool) time.Time {
	if bumped {
		b.s.Bumps++
		b.s.LastBumpAt = &at
	}
	next := earlier(ref.Add(Every), *b.s.EndsAt)
	b.s.NextTickAt = &next
	return next
}

func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func (b *Boost) ID() uuid.UUID        { return b.s.ID }
func (b *Boost) Status() Status       { return b.s.Status }
func (b *Boost) ListingID() uuid.UUID { return b.s.ListingID }
func (b *Boost) SellerID() uuid.UUID  { return b.s.SellerID }
func (b *Boost) PaymentID() uuid.UUID { return b.s.PaymentID }
func (b *Boost) Package() string      { return b.s.Package }

// NextTick is the running boost's next tick; nil when it is not running.
func (b *Boost) NextTick() *time.Time { return b.s.NextTickAt }

// State is every stored field of a boost. Stores only, and views.
type State struct {
	ID         uuid.UUID  `json:"id"`
	ListingID  uuid.UUID  `json:"listing_id"`
	SellerID   uuid.UUID  `json:"seller_id"`
	RequestKey uuid.UUID  `json:"request_key"`
	Package    string     `json:"package"`
	Days       int        `json:"days"`
	Price      int64      `json:"price"`
	Status     Status     `json:"status"`
	PaymentID  uuid.UUID  `json:"payment_id"`
	Bumps      int        `json:"bumps"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	EndsAt     *time.Time `json:"ends_at,omitempty"`
	NextTickAt *time.Time `json:"next_tick_at,omitempty"`
	LastBumpAt *time.Time `json:"last_bump_at,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	WriteID    uuid.UUID  `json:"write_id"`
}

// Restore rebuilds a stored boost. Stores only.
func Restore(st State) Boost { return Boost{s: st} }

// State returns every stored field.
func (b *Boost) State() State { return b.s }

// Saved records that a store wrote b under writeID. Stores only.
func (b *Boost) Saved(writeID uuid.UUID) { b.s.WriteID = writeID }

// MarshalJSON lets a Restate step return a boost for its journal.
func (b Boost) MarshalJSON() ([]byte, error) { return json.Marshal(b.s) }

func (b *Boost) UnmarshalJSON(raw []byte) error { return json.Unmarshal(raw, &b.s) }
