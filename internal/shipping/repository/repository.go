// Package repository stores module shipping's carrier accounts, settings
// and the carriers' events in Postgres. Each method is one statement.
package repository

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/provider/carrier"
)

var ErrNotFound = errors.New("shipping: not found")

// Row is a linked carrier account as stored: the token is sealed, nil for
// a GHN shop ShopNexus joined as staff.
type Row struct {
	AccountID uuid.UUID
	Carrier   string
	ShopCode  string
	Token     []byte
	LinkedAt  time.Time
}

// Accounts persists carrier accounts, one per account and carrier.
type Accounts interface {
	// Put links r, replacing the account's earlier one at r.Carrier.
	Put(ctx context.Context, r Row) error
	// Get returns the owner's account at carrier, or ErrNotFound.
	Get(ctx context.Context, owner uuid.UUID, carrier string) (Row, error)
	// List is a query: the owner's accounts in carrier order.
	List(ctx context.Context, owner uuid.UUID) ([]Row, error)
	// Delete unlinks the owner's account at carrier; false when there was
	// none.
	Delete(ctx context.Context, owner uuid.UUID, carrier string) (bool, error)
	// DeleteAll unlinks every account of the owner's.
	DeleteAll(ctx context.Context, owner uuid.UUID) error
}

// Events keeps what the carriers reported of their parcels, by carrier and
// tracking code.
type Events interface {
	// Add keeps ev unless it repeats one kept: the same kind, step, carrier
	// status and amount within ten minutes, or the newest event of its
	// kind. false when it was a repeat.
	Add(ctx context.Context, c, code string, ev carrier.Event) (bool, error)
	// List is a parcel's events, newest first; its steps alone when
	// stepsOnly.
	List(ctx context.Context, c, code string, stepsOnly bool) ([]carrier.Event, error)
}
