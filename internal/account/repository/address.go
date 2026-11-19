package repository

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
)

// Addresses persists each account's address book. Each method is its own
// transaction. The methods that add, move the default or delete take the
// account's advisory lock, so the cap and the one default hold when they
// race.
type Addresses interface {
	// Insert stores a under the request's key, or refuses an account at
	// domain.MaxAddresses with domain.ErrAddressLimit. a becomes the default
	// when it is the account's first or makeDefault is true, and Insert then
	// calls a.StoredAsDefault. When the account has an address under key
	// already, Insert sets *a to that address and stores nothing, the
	// default included.
	Insert(ctx context.Context, key uuid.UUID, a *domain.Address, makeDefault bool) error
	// Get returns ErrNotFound for an unknown id or an address of another
	// account.
	Get(ctx context.Context, owner, id uuid.UUID) (domain.Address, error)
	// Save writes a's fields when its row still has a.WriteID() or already
	// has writeID, then sets a's write ID to writeID. Otherwise ErrConflict.
	// Moving the default changes a row's write ID too. With makeDefault, a
	// also becomes the default in one transaction that takes the advisory
	// lock and, as Insert does, refuses a deleted account with
	// domain.ErrSessionRevoked; Save then calls a.StoredAsDefault, and a
	// conflict moves nothing.
	Save(ctx context.Context, a *domain.Address, writeID uuid.UUID, makeDefault bool) error
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

// Place is the display names of a commune and its province, and the
// province's code.
type Place struct {
	Commune      string
	Province     string
	ProvinceCode string
}

// PlacedAddress is one address as the list reads it, with its place.
type PlacedAddress struct {
	Address domain.Address
	Place   Place
}
