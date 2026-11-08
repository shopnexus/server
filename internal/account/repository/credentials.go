package repository

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
)

// Credentials persists an account's sign-in methods.
type Credentials interface {
	// SignUp inserts the new account a and its credentials c, with their
	// identities, in one transaction. The same account and credentials again
	// do nothing; an identity another account holds is
	// domain.ErrIdentityTaken and nothing is written.
	SignUp(ctx context.Context, a domain.Account, c domain.Credentials) error
	// Get returns ErrNotFound for an account without credentials.
	Get(ctx context.Context, accountID uuid.UUID) (domain.Credentials, error)
	// FindIdentity returns the identity with kind and value and the account
	// holding it, or ErrNotFound. It is a query, not an aggregate load.
	FindIdentity(ctx context.Context, kind domain.Kind, value string) (domain.IdentityRef, error)
	// Save writes c and replaces its identities when the root still has
	// c.WriteID() or already has writeID. Otherwise ErrConflict; an identity
	// another account holds is domain.ErrIdentityTaken.
	Save(ctx context.Context, c *domain.Credentials, writeID uuid.UUID) error
}
