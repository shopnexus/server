package account

import (
	"context"
	"time"
	"uuid"
)

// Create inserts an active account with the caller's id. Creating the same
// id twice does nothing, so a retried Restate step is safe.
func Create(ctx context.Context, accounts Store, id uuid.UUID, displayName string, now time.Time) error {
	a, err := New(id, displayName, now)
	if err != nil {
		return err
	}
	return accounts.Create(ctx, a)
}
