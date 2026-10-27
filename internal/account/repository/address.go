package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
)

// Addresses persists each account's address book. Each method is its own
// transaction. The methods that add, move the default or delete take the
// account's advisory lock, so the cap and the one default hold when they
// race.
type Addresses interface {
	// InsertAddress stores a under the request's key and calls
	// a.StoredAsDefault when it is the account's first. When the account
	// has an address under key already, it sets *a to that address and
	// stores nothing. An account at domain.MaxAddresses is
	// domain.ErrAddressLimit.
	InsertAddress(ctx context.Context, key uuid.UUID, a *domain.Address) error
	// GetAddress returns ErrNotFound for an unknown id or an address of
	// another account.
	GetAddress(ctx context.Context, owner, id uuid.UUID) (domain.Address, error)
	// SaveAddress writes a's fields when its row still has a.WriteID() or
	// already has writeID, then sets a's write ID to writeID. Otherwise
	// ErrConflict. Moving the default changes a row's write ID too.
	SaveAddress(ctx context.Context, a *domain.Address, writeID uuid.UUID) error
	// SetDefaultAddress makes id owner's default; ErrNotFound when owner
	// has no such address.
	SetDefaultAddress(ctx context.Context, owner, id uuid.UUID) error
	// DeleteAddress removes the row; when it was the default, the newest
	// remaining address becomes the default. ErrNotFound when owner has no
	// such address.
	DeleteAddress(ctx context.Context, owner, id uuid.UUID) error
	// ListAddresses is a query: owner's addresses with their places, the
	// default first, then the newest.
	ListAddresses(ctx context.Context, owner uuid.UUID) ([]AddressRow, error)
	// Commune reads a commune's place; ErrNotFound for an unknown code.
	Commune(ctx context.Context, code string) (Place, error)
}

// Place is the display names of a commune and its province.
type Place struct {
	Commune  string
	Province string
}

// AddressRow is one address as the list reads it.
type AddressRow struct {
	ID          uuid.UUID
	Name        string
	Phone       string
	Street      string
	CommuneCode string
	Commune     string
	Province    string
	IsDefault   bool
	CreatedAt   time.Time
}
