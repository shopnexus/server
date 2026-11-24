// Package repository stores module shipping's carrier accounts in
// Postgres. Each method is one statement.
package repository

import (
	"context"
	"errors"
	"time"
	"uuid"
)

var ErrNotFound = errors.New("shipping: not found")

// Row is a linked carrier account as stored: the token is sealed, nil for
// a GHN shop ShopNexus joined as staff.
type Row struct {
	AccountID uuid.UUID
	Carrier   string
	ShopCode  string
	Token     []byte
	Nonce     []byte
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
