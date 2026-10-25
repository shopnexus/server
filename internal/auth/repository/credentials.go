package repository

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
)

// Credentials persists an account's sign-in methods.
type Credentials interface {
	// Create inserts c with its identities. The same credentials again do
	// nothing; an identity another account holds is domain.ErrTaken and nothing is
	// written.
	Create(ctx context.Context, c domain.Credentials) error
	// Get returns ErrNotFound for an account without credentials.
	Get(ctx context.Context, accountID uuid.UUID) (domain.Credentials, error)
	// FindIdentity returns the identity with kind and value and the account
	// holding it, or ErrNotFound. It is a query, not an aggregate load.
	FindIdentity(ctx context.Context, kind domain.Kind, value string) (domain.IdentityRef, error)
	// Save writes c and replaces its identities when the root still has
	// c.WriteID() or already has writeID. Otherwise ErrConflict; an identity
	// another account holds is domain.ErrTaken.
	Save(ctx context.Context, c *domain.Credentials, writeID uuid.UUID) error
}
