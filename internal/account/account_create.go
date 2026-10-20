package account

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/postgres"
)

// AccountCreate inserts an active account. auth runs it on q inside the
// transaction that writes the first sign-in method, so it has no handler and
// opens no transaction.
type AccountCreate struct {
	Store Store
	Now   func() time.Time
}

func (uc AccountCreate) Execute(ctx context.Context, q postgres.Querier, displayName string) (uuid.UUID, error) {
	a, err := New(displayName, uc.Now())
	if err != nil {
		return uuid.UUID{}, err
	}
	if err := uc.Store.Insert(ctx, q, &a); err != nil {
		return uuid.UUID{}, err
	}
	return a.ID(), nil
}
