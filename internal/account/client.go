package account

import (
	"context"
	"time"
	"uuid"
)

// Client implements the ports other modules declare for accounts.
type Client struct {
	Store Store
	Now   func() time.Time
}

func (c Client) Create(ctx context.Context, id uuid.UUID, displayName string) error {
	return Create(ctx, c.Store, id, displayName, c.Now())
}

func (c Client) Delete(ctx context.Context, id uuid.UUID) error { return Delete(ctx, c.Store, id) }

func (c Client) Status(ctx context.Context, id uuid.UUID) (Status, error) {
	return StatusOf(ctx, c.Store, id, c.Now())
}
