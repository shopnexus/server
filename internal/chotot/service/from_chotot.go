package service

import (
	"context"
	"uuid"
)

// FromChotot reads the listed imports of the account among listings.
func (c *Chotot) FromChotot(ctx context.Context, account uuid.UUID, listings []uuid.UUID) (map[uuid.UUID]bool, error) {
	return c.d.Imports.Listed(ctx, account, listings) //nolint:wrapcheck // the store names it
}
