package repository

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
)

// Addresses persists each account's address book. Each method is its own
// transaction. The methods that add, move the default or delete take the
// account's advisory lock, so the cap and the one default hold when they
// race.
type Addresses interface {
	// Insert stores a under the request's key and calls a.StoredAsDefault
	// when it is the account's first. When the account has an address under
	// key already, it sets *a to that address and stores nothing. An account
	// at domain.MaxAddresses is domain.ErrAddressLimit.
	Insert(ctx context.Context, key uuid.UUID, a *domain.Address) error
	// Get returns ErrNotFound for an unknown id or an address of another
	// account.
	Get(ctx context.Context, owner, id uuid.UUID) (domain.Address, error)
	// Save writes a's fields when its row still has a.WriteID() or already
	// has writeID, then sets a's write ID to writeID. Otherwise ErrConflict.
	// Moving the default changes a row's write ID too.
	Save(ctx context.Context, a *domain.Address, writeID uuid.UUID) error
	// SetDefault makes id owner's default; ErrNotFound when owner has no
	// such address.
	SetDefault(ctx context.Context, owner, id uuid.UUID) error
	// Delete removes the row; when it was the default, the newest remaining
	// address becomes the default. ErrNotFound when owner has no such
	// address.
	Delete(ctx context.Context, owner, id uuid.UUID) error
	// List is a query: owner's addresses with their places, the default
	// first, then the newest.
	List(ctx context.Context, owner uuid.UUID) ([]PlacedAddress, error)
	// Commune reads a commune's place; ErrNotFound for an unknown code.
	Commune(ctx context.Context, code string) (Place, error)
}

// Place is the display names of a commune and its province.
type Place struct {
	Commune  string
	Province string
}

// PlacedAddress is one address as the list reads it, with its place.
type PlacedAddress struct {
	Address domain.Address
	Place   Place
}
