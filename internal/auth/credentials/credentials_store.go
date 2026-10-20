package credentials

import (
	"context"
	"uuid"
)

// Store persists credentials. Each method is its own transaction.
type Store interface {
	// Create inserts c with its identities. The same credentials again do
	// nothing; an identity another account holds is ErrTaken and nothing is
	// written.
	Create(ctx context.Context, c Credentials) error
	// Get returns ErrNotFound for an account without credentials.
	Get(ctx context.Context, accountID uuid.UUID) (Credentials, error)
	// FindIdentity returns the identity with kind and value and the account
	// holding it, or ErrNotFound. It is a query, not an aggregate load.
	FindIdentity(ctx context.Context, kind Kind, value string) (IdentityRef, error)
	// Save writes c and replaces its identities when the root still has
	// c.WriteID() or already has writeID. Otherwise ErrConflict; an identity
	// another account holds is ErrTaken.
	Save(ctx context.Context, c *Credentials, writeID uuid.UUID) error
}
